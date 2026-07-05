package swu

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/iniwex5/vowifi-go/engine/swu/ikev2"
)

var ErrInvalidXFRMConfig = errors.New("invalid swu xfrm config")

type XFRMInterfaceConfig struct {
	Name           string
	OuterDev       string
	IfID           uint32
	MTU            int
	SkipCreateLink bool
}

type KernelXFRMConfig struct {
	ChildSA              ikev2.ChildSAResult
	OuterLocalIP         string
	OuterRemoteIP        string
	InnerLocalPrefix     string
	InnerRemotePrefix    string
	ReqID                int
	Mark                 string
	InterfaceID          uint32
	IncludeForwardPolicy bool
	XFRMInterface        XFRMInterfaceConfig
}

type KernelXFRMState struct {
	undo []ipCommand
}

type LinuxXFRMManager struct {
	Runner IPCommandRunner
}

func (m LinuxXFRMManager) Apply(ctx context.Context, cfg KernelXFRMConfig) (KernelXFRMState, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	runner := m.Runner
	if runner == nil {
		runner = ExecIPCommandRunner{}
	}
	commands, err := buildKernelXFRMCommands(cfg)
	if err != nil {
		return KernelXFRMState{}, err
	}
	var state KernelXFRMState
	for _, command := range commands {
		if err := runner.RunIP(ctx, command.args...); err != nil {
			rollbackErr := runIPUndo(ctx, runner, state.undo)
			if rollbackErr != nil {
				return state, errors.Join(err, rollbackErr)
			}
			return state, err
		}
		if len(command.undo) > 0 {
			state.undo = append(state.undo, ipCommand{args: append([]string(nil), command.undo...)})
		}
	}
	return state, nil
}

func (m LinuxXFRMManager) Cleanup(ctx context.Context, state KernelXFRMState) error {
	if ctx == nil {
		ctx = context.Background()
	}
	runner := m.Runner
	if runner == nil {
		runner = ExecIPCommandRunner{}
	}
	return runIPUndo(ctx, runner, state.undo)
}

func buildKernelXFRMCommands(cfg KernelXFRMConfig) ([]ipCommand, error) {
	params, err := normalizeKernelXFRMConfig(cfg)
	if err != nil {
		return nil, err
	}
	var commands []ipCommand
	if params.xfrmiName != "" {
		if !cfg.XFRMInterface.SkipCreateLink {
			args := []string{"link", "add", params.xfrmiName, "type", "xfrm", "dev", params.xfrmiOuterDev, "if_id", params.ifID}
			commands = append(commands, ipCommand{
				args: args,
				undo: []string{"link", "del", params.xfrmiName},
			})
		}
		if params.xfrmiMTU > 0 {
			commands = append(commands, ipCommand{args: []string{"link", "set", "dev", params.xfrmiName, "mtu", strconv.Itoa(params.xfrmiMTU)}})
		}
		commands = append(commands, ipCommand{args: []string{"link", "set", "dev", params.xfrmiName, "up"}})
	}
	commands = append(commands,
		ipCommand{args: xfrmStateAddArgs(params, true), undo: xfrmStateDelArgs(params, true)},
		ipCommand{args: xfrmStateAddArgs(params, false), undo: xfrmStateDelArgs(params, false)},
		ipCommand{args: xfrmPolicyAddArgs(params, "out"), undo: xfrmPolicyDelArgs(params, "out")},
		ipCommand{args: xfrmPolicyAddArgs(params, "in"), undo: xfrmPolicyDelArgs(params, "in")},
	)
	if params.includeForward {
		commands = append(commands, ipCommand{args: xfrmPolicyAddArgs(params, "fwd"), undo: xfrmPolicyDelArgs(params, "fwd")})
	}
	return commands, nil
}

type kernelXFRMParams struct {
	child          ikev2.ChildSAResult
	outerLocal     string
	outerRemote    string
	innerLocal     string
	innerRemote    string
	reqID          string
	mark           string
	ifID           string
	includeForward bool
	xfrmiName      string
	xfrmiOuterDev  string
	xfrmiMTU       int
}

func normalizeKernelXFRMConfig(cfg KernelXFRMConfig) (kernelXFRMParams, error) {
	outerLocal, err := normalizeIPAddress(cfg.OuterLocalIP, "outer local ip")
	if err != nil {
		return kernelXFRMParams{}, wrapXFRMError(err)
	}
	outerRemote, err := normalizeIPAddress(cfg.OuterRemoteIP, "outer remote ip")
	if err != nil {
		return kernelXFRMParams{}, wrapXFRMError(err)
	}
	innerLocal, err := normalizeIPPrefix(cfg.InnerLocalPrefix, "inner local prefix")
	if err != nil {
		return kernelXFRMParams{}, wrapXFRMError(err)
	}
	innerRemote, err := normalizeIPPrefix(cfg.InnerRemotePrefix, "inner remote prefix")
	if err != nil {
		return kernelXFRMParams{}, wrapXFRMError(err)
	}
	if err := validateChildSAForXFRM(cfg.ChildSA); err != nil {
		return kernelXFRMParams{}, err
	}
	reqID := cfg.ReqID
	if reqID < 0 {
		return kernelXFRMParams{}, fmt.Errorf("%w: reqid must be positive", ErrInvalidXFRMConfig)
	}
	if reqID == 0 {
		reqID = 1
	}
	mark := ""
	if strings.TrimSpace(cfg.Mark) != "" {
		mark, err = normalizeRoutingToken(cfg.Mark, "xfrm mark")
		if err != nil {
			return kernelXFRMParams{}, wrapXFRMError(err)
		}
	}
	ifID := cfg.InterfaceID
	xfrmiName := strings.TrimSpace(cfg.XFRMInterface.Name)
	xfrmiOuterDev := strings.TrimSpace(cfg.XFRMInterface.OuterDev)
	if cfg.XFRMInterface.IfID != 0 {
		ifID = cfg.XFRMInterface.IfID
	}
	if xfrmiName != "" {
		if err := validateRoutingInterfaceName(xfrmiName); err != nil {
			return kernelXFRMParams{}, fmt.Errorf("%w: xfrm interface name: %v", ErrInvalidXFRMConfig, err)
		}
		if xfrmiOuterDev == "" {
			return kernelXFRMParams{}, fmt.Errorf("%w: xfrm interface outer dev is empty", ErrInvalidXFRMConfig)
		}
		if err := validateRoutingInterfaceName(xfrmiOuterDev); err != nil {
			return kernelXFRMParams{}, fmt.Errorf("%w: xfrm interface outer dev: %v", ErrInvalidXFRMConfig, err)
		}
		if ifID == 0 {
			return kernelXFRMParams{}, fmt.Errorf("%w: xfrm interface if_id is zero", ErrInvalidXFRMConfig)
		}
		if cfg.XFRMInterface.MTU < 0 {
			return kernelXFRMParams{}, fmt.Errorf("%w: xfrm interface mtu must be positive", ErrInvalidXFRMConfig)
		}
	}
	return kernelXFRMParams{
		child:          cfg.ChildSA,
		outerLocal:     outerLocal,
		outerRemote:    outerRemote,
		innerLocal:     innerLocal,
		innerRemote:    innerRemote,
		reqID:          strconv.Itoa(reqID),
		mark:           mark,
		ifID:           xfrmID(ifID),
		includeForward: cfg.IncludeForwardPolicy,
		xfrmiName:      xfrmiName,
		xfrmiOuterDev:  xfrmiOuterDev,
		xfrmiMTU:       cfg.XFRMInterface.MTU,
	}, nil
}

func validateChildSAForXFRM(child ikev2.ChildSAResult) error {
	if _, err := xfrmSPI(child.RemoteSPI); err != nil {
		return fmt.Errorf("%w: remote spi: %v", ErrInvalidXFRMConfig, err)
	}
	if _, err := xfrmSPI(child.LocalSPI); err != nil {
		return fmt.Errorf("%w: local spi: %v", ErrInvalidXFRMConfig, err)
	}
	if child.Keys.Profile.EncryptionID != ikev2.ENCR_AES_CBC {
		return fmt.Errorf("%w: unsupported ESP encryption %d", ErrInvalidXFRMConfig, child.Keys.Profile.EncryptionID)
	}
	if _, _, err := xfrmAuthAlgorithm(child.Keys.Profile.IntegrityID); err != nil {
		return err
	}
	if err := validateXFRMKeys(child.Keys.Profile, child.Keys.Outbound, "outbound"); err != nil {
		return err
	}
	if err := validateXFRMKeys(child.Keys.Profile, child.Keys.Inbound, "inbound"); err != nil {
		return err
	}
	return nil
}

func validateXFRMKeys(profile ikev2.ESPKeyProfile, keys ikev2.ESPKeys, direction string) error {
	if len(keys.EncryptionKey) != 16 && len(keys.EncryptionKey) != 24 && len(keys.EncryptionKey) != 32 {
		return fmt.Errorf("%w: %s AES key length %d", ErrInvalidXFRMConfig, direction, len(keys.EncryptionKey))
	}
	if profile.EncryptionKeyLength > 0 && len(keys.EncryptionKey) != profile.EncryptionKeyLength {
		return fmt.Errorf("%w: %s encryption key length %d != %d", ErrInvalidXFRMConfig, direction, len(keys.EncryptionKey), profile.EncryptionKeyLength)
	}
	if len(keys.IntegrityKey) == 0 {
		return fmt.Errorf("%w: %s integrity key is empty", ErrInvalidXFRMConfig, direction)
	}
	if profile.IntegrityKeyLength > 0 && len(keys.IntegrityKey) != profile.IntegrityKeyLength {
		return fmt.Errorf("%w: %s integrity key length %d != %d", ErrInvalidXFRMConfig, direction, len(keys.IntegrityKey), profile.IntegrityKeyLength)
	}
	return nil
}

func xfrmStateAddArgs(params kernelXFRMParams, outbound bool) []string {
	src, dst, spi, keys := xfrmDirection(params, outbound)
	authAlg, truncBits, _ := xfrmAuthAlgorithm(params.child.Keys.Profile.IntegrityID)
	args := []string{
		"xfrm", "state", "add",
		"src", src,
		"dst", dst,
		"proto", "esp",
		"spi", spi,
		"reqid", params.reqID,
		"mode", "tunnel",
		"auth-trunc", authAlg, xfrmHexKey(keys.IntegrityKey), strconv.Itoa(truncBits),
		"enc", "cbc(aes)", xfrmHexKey(keys.EncryptionKey),
	}
	args = appendXFRMCommonSelectors(args, params)
	return args
}

func xfrmStateDelArgs(params kernelXFRMParams, outbound bool) []string {
	src, dst, spi, _ := xfrmDirection(params, outbound)
	args := []string{"xfrm", "state", "delete", "src", src, "dst", dst, "proto", "esp", "spi", spi}
	args = appendXFRMCommonSelectors(args, params)
	return args
}

func xfrmPolicyAddArgs(params kernelXFRMParams, dir string) []string {
	src, dst, tmplSrc, tmplDst := xfrmPolicyDirection(params, dir)
	args := []string{
		"xfrm", "policy", "add",
		"src", src,
		"dst", dst,
		"dir", dir,
	}
	args = appendXFRMCommonSelectors(args, params)
	args = append(args,
		"tmpl",
		"src", tmplSrc,
		"dst", tmplDst,
		"proto", "esp",
		"reqid", params.reqID,
		"mode", "tunnel",
	)
	return args
}

func xfrmPolicyDelArgs(params kernelXFRMParams, dir string) []string {
	src, dst, _, _ := xfrmPolicyDirection(params, dir)
	args := []string{
		"xfrm", "policy", "delete",
		"src", src,
		"dst", dst,
		"dir", dir,
	}
	args = appendXFRMCommonSelectors(args, params)
	return args
}

func xfrmDirection(params kernelXFRMParams, outbound bool) (src, dst, spi string, keys ikev2.ESPKeys) {
	if outbound {
		spi, _ = xfrmSPI(params.child.RemoteSPI)
		return params.outerLocal, params.outerRemote, spi, params.child.Keys.Outbound
	}
	spi, _ = xfrmSPI(params.child.LocalSPI)
	return params.outerRemote, params.outerLocal, spi, params.child.Keys.Inbound
}

func xfrmPolicyDirection(params kernelXFRMParams, dir string) (src, dst, tmplSrc, tmplDst string) {
	if dir == "out" {
		return params.innerLocal, params.innerRemote, params.outerLocal, params.outerRemote
	}
	return params.innerRemote, params.innerLocal, params.outerRemote, params.outerLocal
}

func appendXFRMCommonSelectors(args []string, params kernelXFRMParams) []string {
	if params.mark != "" {
		args = append(args, "mark", params.mark)
	}
	if params.ifID != "" {
		args = append(args, "if_id", params.ifID)
	}
	return args
}

func xfrmSPI(spi []byte) (string, error) {
	if len(spi) != 4 {
		return "", fmt.Errorf("spi length %d", len(spi))
	}
	v := binary.BigEndian.Uint32(spi)
	if v == 0 {
		return "", errors.New("spi is zero")
	}
	return fmt.Sprintf("0x%08x", v), nil
}

func xfrmHexKey(key []byte) string {
	return "0x" + hex.EncodeToString(key)
}

func xfrmAuthAlgorithm(integrity uint16) (name string, truncBits int, err error) {
	switch integrity {
	case ikev2.INTEG_HMAC_SHA1_96:
		return "hmac(sha1)", 96, nil
	case ikev2.INTEG_HMAC_SHA2_256_128:
		return "hmac(sha256)", 128, nil
	default:
		return "", 0, fmt.Errorf("%w: unsupported ESP integrity %d", ErrInvalidXFRMConfig, integrity)
	}
}

func xfrmID(id uint32) string {
	if id == 0 {
		return ""
	}
	return fmt.Sprintf("0x%x", id)
}

func wrapXFRMError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrInvalidXFRMConfig) {
		return err
	}
	return fmt.Errorf("%w: %v", ErrInvalidXFRMConfig, err)
}
