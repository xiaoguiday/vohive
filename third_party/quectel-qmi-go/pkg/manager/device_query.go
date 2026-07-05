package manager

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/iniwex5/quectel-qmi-go/pkg/qmi"
)

// ============================================================================
// 设备信息查询方法 — 将底层 DMS/UIM/NAS/WMS 服务的能力暴露给上层调用者
// ============================================================================

// PreWarmIdentities 异步并发抓取设备基础标识填充到 Snapshot。不会阻塞当前线程。
func (m *Manager) PreWarmIdentities(forceAll bool) {
	if m == nil {
		return
	}
	generation := m.snapshot.IdentityGeneration()
	go func(gen uint64) {
		// 给予足够的超时时间，避免后台抓取时与其他初始化流程抢占导致超时
		ctx, cancel := contextWithMaxTimeout(context.Background(), 10*time.Second)
		defer cancel()

		var wg sync.WaitGroup
		var ids DeviceIdentities
		var lock sync.Mutex // 保护此层局部变量写并发

		current, staticReady := m.snapshot.Identities()
		needHW := forceAll || !staticReady || current.IMEI == ""

		if needHW {
			wg.Add(1)
			go func() {
				defer wg.Done()
				// DMS 获取硬件信息
				if devInfo, err := m.GetDeviceSerialNumbers(ctx); err == nil && devInfo != nil {
					lock.Lock()
					ids.IMEI = devInfo.IMEI
					lock.Unlock()
				}
				if fw, hw, err := m.GetDeviceRevision(ctx); err == nil {
					lock.Lock()
					ids.FirmwareRevision = fw
					ids.HardwareRevision = hw
					lock.Unlock()
				}
				if manufacturer, err := m.GetManufacturer(ctx); err == nil {
					lock.Lock()
					ids.Manufacturer = manufacturer
					lock.Unlock()
				}
				if model, err := m.GetModel(ctx); err == nil {
					lock.Lock()
					ids.Model = model
					lock.Unlock()
				}
				if mode, err := m.GetOperatingMode(ctx); err == nil {
					lock.Lock()
					ids.OperatingMode = &mode
					lock.Unlock()
				}
			}()
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			// 获取卡关联信息
			if iccid, err := m.GetICCID(ctx); err == nil {
				lock.Lock()
				ids.ICCID = iccid
				lock.Unlock()
			}
			if imsi, err := m.GetIMSI(ctx); err == nil {
				lock.Lock()
				ids.IMSI = imsi
				lock.Unlock()
			}
			if simStatus, err := m.GetSIMStatus(ctx); err == nil {
				lock.Lock()
				inserted := simStatus != qmi.SIMAbsent
				ids.SimInserted = &inserted
				lock.Unlock()
			}
		}()

		wg.Wait()
		if !m.snapshot.UpdateIdentitiesIfGeneration(ids, gen) {
			m.log.WithField("generation", gen).Debug("Skip stale device identities pre-warm write")
			return
		}
		m.log.WithField("imei", ids.IMEI).WithField("iccid", ids.ICCID).Debug("Device identities pre-warmed")
	}(generation)
}

// GetCachedIdentities 提供给上层应用零 IPC 读取当前设备的基础与卡标识。
func (m *Manager) GetCachedIdentities() (DeviceIdentities, bool) {
	if m == nil {
		return DeviceIdentities{}, false
	}
	return m.snapshot.Identities()
}

func (m *Manager) GetCachedIdentitiesReadiness() (bool, bool) {
	if m == nil {
		return false, false
	}
	return m.snapshot.IdentityReadiness()
}

// CTLGetServiceVersions 获取底层通信 Client 缓存的 QMI 服务版本信息。
// 仅在 client 成功初始化后调用有效，否则返回 nil。
func (m *Manager) CTLGetServiceVersions() map[uint8]qmi.ServiceVersion {
	if m == nil || m.client == nil {
		return nil
	}
	return m.client.GetCachedServiceVersions()
}

// GetDeviceSerialNumbers 获取设备序列号信息（含 IMEI）
func (m *Manager) GetDeviceSerialNumbers(ctx context.Context) (*qmi.DeviceInfo, error) {
	return withDMSRecoveryValue(m, "GetDeviceSerialNumbers", func(dms *qmi.DMSService) (*qmi.DeviceInfo, error) {
		return dms.GetDeviceSerialNumbers(ctx)
	})
}

// GetDeviceRevision 获取设备固件版本
func (m *Manager) GetDeviceRevision(ctx context.Context) (string, string, error) {
	type revision struct {
		fw string
		hw string
	}
	rev, err := withDMSRecoveryValue(m, "GetDeviceRevision", func(dms *qmi.DMSService) (revision, error) {
		fw, hw, callErr := dms.GetDeviceRevision(ctx)
		return revision{fw: fw, hw: hw}, callErr
	})
	if err != nil {
		return "", "", err
	}
	return rev.fw, rev.hw, nil
}

// GetManufacturer 获取模组厂商
func (m *Manager) GetManufacturer(ctx context.Context) (string, error) {
	return withDMSRecoveryValue(m, "GetManufacturer", func(dms *qmi.DMSService) (string, error) {
		return dms.GetManufacturer(ctx)
	})
}

// GetModel 获取模组型号
func (m *Manager) GetModel(ctx context.Context) (string, error) {
	return withDMSRecoveryValue(m, "GetModel", func(dms *qmi.DMSService) (string, error) {
		return dms.GetModel(ctx)
	})
}

// GetHardwareRevision 获取硬件版本
func (m *Manager) GetHardwareRevision(ctx context.Context) (string, error) {
	return withDMSRecoveryValue(m, "GetHardwareRevision", func(dms *qmi.DMSService) (string, error) {
		return dms.GetHardwareRevision(ctx)
	})
}

// GetSoftwareVersion 获取软件版本
func (m *Manager) GetSoftwareVersion(ctx context.Context) (string, error) {
	return withDMSRecoveryValue(m, "GetSoftwareVersion", func(dms *qmi.DMSService) (string, error) {
		return dms.GetSoftwareVersion(ctx)
	})
}

// GetMSISDN 获取设备关联的 MSISDN
func (m *Manager) GetMSISDN(ctx context.Context) (string, error) {
	return withDMSRecoveryValue(m, "GetMSISDN", func(dms *qmi.DMSService) (string, error) {
		return dms.GetMSISDN(ctx)
	})
}

// GetFactorySKU 获取出厂 SKU
func (m *Manager) GetFactorySKU(ctx context.Context) (string, error) {
	return withDMSRecoveryValue(m, "GetFactorySKU", func(dms *qmi.DMSService) (string, error) {
		return dms.GetFactorySKU(ctx)
	})
}

// GetCapabilities 获取模组总体能力信息
func (m *Manager) GetCapabilities(ctx context.Context) (*qmi.DeviceCapabilities, error) {
	return withDMSRecoveryValue(m, "GetCapabilities", func(dms *qmi.DMSService) (*qmi.DeviceCapabilities, error) {
		return dms.GetCapabilities(ctx)
	})
}

// GetPowerState 获取供电与电池状态
func (m *Manager) GetPowerState(ctx context.Context) (*qmi.PowerStateInfo, error) {
	return withDMSRecoveryValue(m, "GetPowerState", func(dms *qmi.DMSService) (*qmi.PowerStateInfo, error) {
		return dms.GetPowerState(ctx)
	})
}

// GetTime 获取模组当前时间计数
func (m *Manager) GetTime(ctx context.Context) (*qmi.TimeInfo, error) {
	return withDMSRecoveryValue(m, "GetTime", func(dms *qmi.DMSService) (*qmi.TimeInfo, error) {
		return dms.GetTime(ctx)
	})
}

// GetPRLVersion 获取 PRL 版本信息
func (m *Manager) GetPRLVersion(ctx context.Context) (*qmi.PRLVersionInfo, error) {
	return withDMSRecoveryValue(m, "GetPRLVersion", func(dms *qmi.DMSService) (*qmi.PRLVersionInfo, error) {
		return dms.GetPRLVersion(ctx)
	})
}

// GetActivationState 获取业务激活状态
func (m *Manager) GetActivationState(ctx context.Context) (qmi.ActivationState, error) {
	return withDMSRecoveryValue(m, "GetActivationState", func(dms *qmi.DMSService) (qmi.ActivationState, error) {
		return dms.GetActivationState(ctx)
	})
}

// GetUserLockState 获取用户锁状态
func (m *Manager) GetUserLockState(ctx context.Context) (bool, error) {
	return withDMSRecoveryValue(m, "GetUserLockState", func(dms *qmi.DMSService) (bool, error) {
		return dms.GetUserLockState(ctx)
	})
}

// SetUserLockState 设置用户锁状态
func (m *Manager) SetUserLockState(ctx context.Context, enabled bool, lockCode string) error {
	return m.withDMSRecovery("SetUserLockState", func(dms *qmi.DMSService) error {
		return dms.SetUserLockState(ctx, enabled, lockCode)
	})
}

// SetUserLockCode 修改用户锁码
func (m *Manager) SetUserLockCode(ctx context.Context, oldCode, newCode string) error {
	return m.withDMSRecovery("SetUserLockCode", func(dms *qmi.DMSService) error {
		return dms.SetUserLockCode(ctx, oldCode, newCode)
	})
}

// ReadUserData 读取设备用户数据
func (m *Manager) ReadUserData(ctx context.Context) ([]byte, error) {
	return withDMSRecoveryValue(m, "ReadUserData", func(dms *qmi.DMSService) ([]byte, error) {
		return dms.ReadUserData(ctx)
	})
}

// WriteUserData 写入设备用户数据
func (m *Manager) WriteUserData(ctx context.Context, data []byte) error {
	return m.withDMSRecovery("WriteUserData", func(dms *qmi.DMSService) error {
		return dms.WriteUserData(ctx, data)
	})
}

// GetMACAddress 获取指定类型的 MAC 地址
func (m *Manager) GetMACAddress(ctx context.Context, macType uint32) (*qmi.MACAddressInfo, error) {
	return withDMSRecoveryValue(m, "GetMACAddress", func(dms *qmi.DMSService) (*qmi.MACAddressInfo, error) {
		return dms.GetMACAddress(ctx, macType)
	})
}

// GetBandCapabilities 获取模组支持的频段能力
func (m *Manager) GetBandCapabilities(ctx context.Context) (*qmi.BandCapabilities, error) {
	return withDMSRecoveryValue(m, "GetBandCapabilities", func(dms *qmi.DMSService) (*qmi.BandCapabilities, error) {
		return dms.GetBandCapabilities(ctx)
	})
}

// GetIMSI 获取 SIM 卡 IMSI / GetIMSI retrieves SIM IMSI via fallback strategy
func (m *Manager) GetIMSI(ctx context.Context) (string, error) {
	return m.GetIMSIStrictLive(ctx)
}

// GetIMSIStrictLive 严格实时读取 IMSI（不依赖 snapshot 缓存）。
func (m *Manager) GetIMSIStrictLive(ctx context.Context) (string, error) {
	if m != nil && m.getIMSIStrictHook != nil {
		return m.getIMSIStrictHook(ctx)
	}

	var lastErr error

	// 优先尝试 DMS 获取 (DMS 通常最稳定且自带缓存)
	imsi, err := withDMSRecoveryValue(m, "GetIMSI.DMS", func(dms *qmi.DMSService) (string, error) {
		return dms.GetIMSI(ctx)
	})
	if err == nil && imsi != "" {
		return imsi, nil
	}
	lastErr = err

	// 降级尝试 UIM 透明获取
	imsi, err = withUIMRecoveryValue(m, "GetIMSI.UIM", func(uim *qmi.UIMService) (string, error) {
		return uim.GetIMSI(ctx)
	})
	if err == nil && imsi != "" {
		return imsi, nil
	}
	return "", fmt.Errorf("DMS & UIM 双通道均无法获取 IMSI (UIM error: %v, DMS lastError: %v)", err, lastErr)
}

// GetICCID 获取 SIM 卡 ICCID
func (m *Manager) GetICCID(ctx context.Context) (string, error) {
	return m.GetICCIDStrictLive(ctx)
}

// GetICCIDStrictLive 严格实时读取 ICCID（不依赖 snapshot 缓存）。
func (m *Manager) GetICCIDStrictLive(ctx context.Context) (string, error) {
	if m != nil && m.getICCIDStrictHook != nil {
		return m.getICCIDStrictHook(ctx)
	}

	var lastErr error
	iccid, err := withDMSRecoveryValue(m, "GetICCID.DMS", func(dms *qmi.DMSService) (string, error) {
		return dms.GetICCID(ctx)
	})
	if err == nil && iccid != "" {
		return iccid, nil
	}
	lastErr = err

	iccid, err = withUIMRecoveryValue(m, "GetICCID", func(uim *qmi.UIMService) (string, error) {
		return uim.GetICCID(ctx)
	})
	if err == nil && iccid != "" {
		return iccid, nil
	}
	return "", fmt.Errorf("DMS & UIM 双通道均无法获取 ICCID (UIM error: %v, DMS lastError: %v)", err, lastErr)
}

// UIMGetSlotStatus 获取物理/逻辑卡槽状态
func (m *Manager) UIMGetSlotStatus(ctx context.Context) (*qmi.UIMSlotStatus, error) {
	return withUIMRecoveryValue(m, "UIMGetSlotStatus", func(uim *qmi.UIMService) (*qmi.UIMSlotStatus, error) {
		return uim.GetSlotStatus(ctx)
	})
}

// UIMSwitchSlot 切换逻辑 slot 到目标物理 slot
func (m *Manager) UIMSwitchSlot(ctx context.Context, logicalSlot uint8, physicalSlot uint32) error {
	return m.withUIMRecovery("UIMSwitchSlot", func(uim *qmi.UIMService) error {
		return uim.SwitchSlot(ctx, logicalSlot, physicalSlot)
	})
}

// UIMReadRecord 读取 record 型 EF 文件
func (m *Manager) UIMReadRecord(ctx context.Context, fileID uint16, path []uint8, recordNumber uint16, recordLength uint16) (*qmi.UIMRecordData, error) {
	return withUIMRecoveryValue(m, "UIMReadRecord", func(uim *qmi.UIMService) (*qmi.UIMRecordData, error) {
		return uim.ReadRecord(ctx, fileID, path, recordNumber, recordLength)
	})
}

// UIMReadRecordWithSession 使用指定 session 读取 record 型 EF 文件
func (m *Manager) UIMReadRecordWithSession(ctx context.Context, sessionType uint8, fileID uint16, path []uint8, recordNumber uint16, recordLength uint16) (*qmi.UIMRecordData, error) {
	return withUIMRecoveryValue(m, "UIMReadRecordWithSession", func(uim *qmi.UIMService) (*qmi.UIMRecordData, error) {
		return uim.ReadRecordWithSession(ctx, sessionType, fileID, path, recordNumber, recordLength)
	})
}

// UIMGetFileAttributes 获取 SIM 文件元数据
func (m *Manager) UIMGetFileAttributes(ctx context.Context, fileID uint16, path []uint8) (*qmi.UIMFileAttributes, error) {
	return withUIMRecoveryValue(m, "UIMGetFileAttributes", func(uim *qmi.UIMService) (*qmi.UIMFileAttributes, error) {
		return uim.GetFileAttributes(ctx, fileID, path)
	})
}

// UIMGetFileAttributesWithSession 使用指定 session 获取 SIM 文件元数据
func (m *Manager) UIMGetFileAttributesWithSession(ctx context.Context, sessionType uint8, fileID uint16, path []uint8) (*qmi.UIMFileAttributes, error) {
	return withUIMRecoveryValue(m, "UIMGetFileAttributesWithSession", func(uim *qmi.UIMService) (*qmi.UIMFileAttributes, error) {
		return uim.GetFileAttributesWithSession(ctx, sessionType, fileID, path)
	})
}

// UIMReadTransparentWithSession 使用指定 session 读取 transparent 型 EF 文件
func (m *Manager) UIMReadTransparentWithSession(ctx context.Context, sessionType uint8, fileID uint16, path []uint8) ([]byte, error) {
	return withUIMRecoveryValue(m, "UIMReadTransparentWithSession", func(uim *qmi.UIMService) ([]byte, error) {
		return uim.ReadTransparentWithSession(ctx, sessionType, fileID, path)
	})
}

// UIMRegisterEvents 注册 UIM 事件掩码
func (m *Manager) UIMRegisterEvents(ctx context.Context, mask uint32) (uint32, error) {
	return withUIMRecoveryValue(m, "UIMRegisterEvents", func(uim *qmi.UIMService) (uint32, error) {
		return uim.RegisterEvents(ctx, mask)
	})
}

// UIMGetSupportedMessages 获取 UIM service 支持的消息 ID
func (m *Manager) UIMGetSupportedMessages(ctx context.Context) ([]uint8, error) {
	return withUIMRecoveryValue(m, "UIMGetSupportedMessages", func(uim *qmi.UIMService) ([]uint8, error) {
		return uim.GetSupportedMessages(ctx)
	})
}

// UIMReset 重置 UIM service 状态
func (m *Manager) UIMReset(ctx context.Context) error {
	return m.withUIMRecovery("UIMReset", func(uim *qmi.UIMService) error {
		return uim.Reset(ctx)
	})
}

// UIMPowerOffSIM 关闭指定 slot 的 SIM 电源
func (m *Manager) UIMPowerOffSIM(ctx context.Context, slot uint8) error {
	return m.withUIMRecovery("UIMPowerOffSIM", func(uim *qmi.UIMService) error {
		return uim.PowerOffSIM(ctx, slot)
	})
}

// UIMPowerOnSIM 打开指定 slot 的 SIM 电源
func (m *Manager) UIMPowerOnSIM(ctx context.Context, slot uint8) error {
	return m.withUIMRecovery("UIMPowerOnSIM", func(uim *qmi.UIMService) error {
		return uim.PowerOnSIM(ctx, slot)
	})
}

// UIMChangeProvisioningSession 切换 UIM provisioning session
func (m *Manager) UIMChangeProvisioningSession(ctx context.Context, req qmi.UIMChangeProvisioningSessionRequest) error {
	return m.withUIMRecovery("UIMChangeProvisioningSession", func(uim *qmi.UIMService) error {
		return uim.ChangeProvisioningSession(ctx, req)
	})
}

type UIMPostSwitchReloadOptions struct {
	DefaultSlot    uint8
	PowerCycleWait time.Duration
}

func normalizeUIMPostSwitchReloadOptions(opts UIMPostSwitchReloadOptions) UIMPostSwitchReloadOptions {
	if opts.DefaultSlot == 0 {
		opts.DefaultSlot = 1
	}
	if opts.PowerCycleWait <= 0 {
		opts.PowerCycleWait = 500 * time.Millisecond
	}
	return opts
}

func resolveUIMReloadSlot(readiness UIMReadiness, defaultSlot uint8) uint8 {
	if readiness.SlotKnown && readiness.ActiveSlot != 0 {
		return readiness.ActiveSlot
	}
	if defaultSlot != 0 {
		return defaultSlot
	}
	return 1
}

func (m *Manager) UIMPowerCycleSIM(ctx context.Context, slot uint8, wait time.Duration) error {
	if slot == 0 {
		slot = 1
	}
	if wait <= 0 {
		wait = 500 * time.Millisecond
	}
	if err := m.UIMPowerOffSIM(ctx, slot); err != nil {
		return fmt.Errorf("UIMPowerOffSIM(slot=%d): %w", slot, err)
	}
	if err := sleepWithContext(ctx, wait); err != nil {
		return err
	}
	if err := m.UIMPowerOnSIM(ctx, slot); err != nil {
		return fmt.Errorf("UIMPowerOnSIM(slot=%d): %w", slot, err)
	}
	if err := sleepWithContext(ctx, wait); err != nil {
		return err
	}
	return nil
}

func (m *Manager) UIMRebindPrimaryGWProvisioning(ctx context.Context, slot uint8, usimAID []byte) error {
	return uimRebindPrimaryGWProvisioningWithSender(ctx, slot, usimAID, 200*time.Millisecond, m.UIMChangeProvisioningSession)
}

func uimRebindPrimaryGWProvisioningWithSender(ctx context.Context, slot uint8, usimAID []byte, wait time.Duration, send func(context.Context, qmi.UIMChangeProvisioningSessionRequest) error) error {
	if slot == 0 {
		slot = 1
	}
	if len(usimAID) == 0 {
		return fmt.Errorf("USIM AID is required for provisioning rebind")
	}
	if send == nil {
		return fmt.Errorf("UIM provisioning sender unavailable")
	}
	if err := send(ctx, qmi.UIMChangeProvisioningSessionRequest{
		SessionType: qmi.UIMSessionTypePrimaryGWProvisioning,
		Activate:    false,
	}); err != nil {
		return fmt.Errorf("deactivate PrimaryGW provisioning: %w", err)
	}
	if err := sleepWithContext(ctx, wait); err != nil {
		return err
	}
	if err := send(ctx, qmi.UIMChangeProvisioningSessionRequest{
		SessionType:           qmi.UIMSessionTypePrimaryGWProvisioning,
		Activate:              true,
		Slot:                  &slot,
		ApplicationIdentifier: append([]byte(nil), usimAID...),
	}); err != nil {
		return fmt.Errorf("activate PrimaryGW provisioning: %w", err)
	}
	return nil
}

func (m *Manager) UIMPostSwitchReload(ctx context.Context, readiness UIMReadiness, opts UIMPostSwitchReloadOptions) (uint8, error) {
	opts = normalizeUIMPostSwitchReloadOptions(opts)
	slot := resolveUIMReloadSlot(readiness, opts.DefaultSlot)
	if err := m.UIMPowerCycleSIM(ctx, slot, opts.PowerCycleWait); err != nil {
		return slot, err
	}
	if _, err := m.EnsureSIMProvisioned(ctx, EnsureSIMProvisionedOptions{
		DefaultSlot:      slot,
		MaxAttempts:      4,
		MaxActivations:   1,
		PollInterval:     400 * time.Millisecond,
		ActivationSettle: 1500 * time.Millisecond,
	}); err != nil {
		return slot, err
	}
	return slot, nil
}

func sleepWithContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// UIMRefreshRegister 注册 UIM refresh 文件列表
func (m *Manager) UIMRefreshRegister(ctx context.Context, req qmi.UIMRefreshRegisterRequest) error {
	return m.withUIMRecovery("UIMRefreshRegister", func(uim *qmi.UIMService) error {
		return uim.RefreshRegister(ctx, req)
	})
}

// UIMRefreshComplete 上报 UIM refresh 处理完成
func (m *Manager) UIMRefreshComplete(ctx context.Context, req qmi.UIMRefreshCompleteRequest) error {
	return m.withUIMRecovery("UIMRefreshComplete", func(uim *qmi.UIMService) error {
		return uim.RefreshComplete(ctx, req)
	})
}

// UIMRefreshRegisterAll 注册 UIM 全文件 refresh
func (m *Manager) UIMRefreshRegisterAll(ctx context.Context, req qmi.UIMRefreshRegisterAllRequest) error {
	return m.withUIMRecovery("UIMRefreshRegisterAll", func(uim *qmi.UIMService) error {
		return uim.RefreshRegisterAll(ctx, req)
	})
}

// GetSIMStatus 获取 SIM 卡状态
func (m *Manager) GetSIMStatus(ctx context.Context) (qmi.SIMStatus, error) {
	return withDMSRecoveryValue(m, "GetSIMStatus", func(dms *qmi.DMSService) (qmi.SIMStatus, error) {
		return dms.GetSIMStatus(ctx)
	})
}

// GetServingSystem 获取当前网络服务系统信息
func (m *Manager) GetServingSystem(ctx context.Context) (*qmi.ServingSystem, error) {
	return withNASRecoveryValue(m, "GetServingSystem", func(nas *qmi.NASService) (*qmi.ServingSystem, error) {
		return nas.GetServingSystem(ctx)
	})
}

// GetSignalStrength 获取信号强度
func (m *Manager) GetSignalStrength(ctx context.Context) (*qmi.SignalStrength, error) {
	return withNASRecoveryValue(m, "GetSignalStrength", func(nas *qmi.NASService) (*qmi.SignalStrength, error) {
		return nas.GetSignalStrength(ctx)
	})
}

// GetSignalInfo 获取详细信号信息（LTE/5G）
func (m *Manager) GetSignalInfo(ctx context.Context) (*qmi.SignalInfo, error) {
	return withNASRecoveryValue(m, "GetSignalInfo", func(nas *qmi.NASService) (*qmi.SignalInfo, error) {
		return nas.GetSignalInfo(ctx)
	})
}

// GetSysInfo 获取系统信息（CellID/TAC/LAC）
func (m *Manager) GetSysInfo(ctx context.Context) (*qmi.SysInfo, error) {
	return withNASRecoveryValue(m, "GetSysInfo", func(nas *qmi.NASService) (*qmi.SysInfo, error) {
		return nas.GetSysInfo(ctx)
	})
}

// NASGetRFBandInfo 获取当前频段与信道信息
func (m *Manager) NASGetRFBandInfo(ctx context.Context) (*qmi.RFBandInfo, error) {
	return withNASRecoveryValue(m, "NASGetRFBandInfo", func(nas *qmi.NASService) (*qmi.RFBandInfo, error) {
		return nas.GetRFBandInfo(ctx)
	})
}

// NASGetTechnologyPreference 获取当前 RAT 偏好
func (m *Manager) NASGetTechnologyPreference(ctx context.Context) (*qmi.TechnologyPreference, error) {
	return withNASRecoveryValue(m, "NASGetTechnologyPreference", func(nas *qmi.NASService) (*qmi.TechnologyPreference, error) {
		return nas.GetTechnologyPreference(ctx)
	})
}

// NASSetTechnologyPreference 设置当前 RAT 偏好
func (m *Manager) NASSetTechnologyPreference(ctx context.Context, pref qmi.TechnologyPreference) error {
	return m.withNASRecovery("NASSetTechnologyPreference", func(nas *qmi.NASService) error {
		return nas.SetTechnologyPreference(ctx, pref)
	})
}

// NASGetSystemSelectionPreference 获取系统选择策略
func (m *Manager) NASGetSystemSelectionPreference(ctx context.Context) (*qmi.SystemSelectionPreference, error) {
	return withNASRecoveryValue(m, "NASGetSystemSelectionPreference", func(nas *qmi.NASService) (*qmi.SystemSelectionPreference, error) {
		return nas.GetSystemSelectionPreference(ctx)
	})
}

// NASSetSystemSelectionPreference 设置系统选择策略
func (m *Manager) NASSetSystemSelectionPreference(ctx context.Context, pref qmi.SystemSelectionPreference) error {
	return m.withNASRecovery("NASSetSystemSelectionPreference", func(nas *qmi.NASService) error {
		return nas.SetSystemSelectionPreference(ctx, pref)
	})
}

// NASGetCellLocationInfo 获取当前小区位置与制式信息
func (m *Manager) NASGetCellLocationInfo(ctx context.Context) (*qmi.CellLocationInfo, error) {
	return withNASRecoveryValue(m, "NASGetCellLocationInfo", func(nas *qmi.NASService) (*qmi.CellLocationInfo, error) {
		return nas.GetCellLocationInfo(ctx)
	})
}

// NASGetNetworkTime 获取网络时间
func (m *Manager) NASGetNetworkTime(ctx context.Context) (*qmi.NetworkTimeInfo, error) {
	return withNASRecoveryValue(m, "NASGetNetworkTime", func(nas *qmi.NASService) (*qmi.NetworkTimeInfo, error) {
		return nas.GetNetworkTime(ctx)
	})
}

// NASInitiateNetworkRegister 发起自动/手动驻网
func (m *Manager) NASInitiateNetworkRegister(ctx context.Context, req qmi.NASInitiateNetworkRegisterRequest) error {
	return m.withNASRecovery("NASInitiateNetworkRegister", func(nas *qmi.NASService) error {
		return nas.InitiateNetworkRegister(ctx, req)
	})
}

// NASForceNetworkSearch 强制 modem 重新搜网
func (m *Manager) NASForceNetworkSearch(ctx context.Context) error {
	return m.withNASRecovery("NASForceNetworkSearch", func(nas *qmi.NASService) error {
		return nas.ForceNetworkSearch(ctx)
	})
}

// NASAttachDetach 设置 PS 附着状态
func (m *Manager) NASAttachDetach(ctx context.Context, attached bool) error {
	return m.withNASRecovery("NASAttachDetach", func(nas *qmi.NASService) error {
		return nas.AttachDetach(ctx, attached)
	})
}

// NASGetOperatorName 获取当前运营商名称
func (m *Manager) NASGetOperatorName(ctx context.Context) (*qmi.NASOperatorNameInfo, error) {
	return withNASRecoveryValue(m, "NASGetOperatorName", func(nas *qmi.NASService) (*qmi.NASOperatorNameInfo, error) {
		return nas.GetOperatorName(ctx)
	})
}

// NASGetPLMNName 获取指定 PLMN 的长短名称
func (m *Manager) NASGetPLMNName(ctx context.Context, req qmi.NASPLMNNameRequest) (*qmi.NASPLMNNameInfo, error) {
	return withNASRecoveryValue(m, "NASGetPLMNName", func(nas *qmi.NASService) (*qmi.NASPLMNNameInfo, error) {
		return nas.GetPLMNName(ctx, req)
	})
}

// NASConfigSignalInfoV2 配置信号变化上报阈值
func (m *Manager) NASConfigSignalInfoV2(ctx context.Context, cfg qmi.NASSignalInfoConfigV2) error {
	return m.withNASRecovery("NASConfigSignalInfoV2", func(nas *qmi.NASService) error {
		return nas.ConfigSignalInfoV2(ctx, cfg)
	})
}

// NASRegisterIndications 注册 NAS indication 上报开关
func (m *Manager) NASRegisterIndications(ctx context.Context, cfg qmi.NASIndicationRegistration) error {
	return m.withNASRecovery("NASRegisterIndications", func(nas *qmi.NASService) error {
		return nas.RegisterIndicationsWithConfig(ctx, cfg)
	})
}

// GetOperatingMode 获取设备当前操作模式
func (m *Manager) GetOperatingMode(ctx context.Context) (qmi.OperatingMode, error) {
	return withDMSRecoveryValue(m, "GetOperatingMode", func(dms *qmi.DMSService) (qmi.OperatingMode, error) {
		return dms.GetOperatingMode(ctx)
	})
}

// SetOperatingMode 设置设备操作模式（飞行模式 / 在线 / 低功耗等）
func (m *Manager) SetOperatingMode(ctx context.Context, mode qmi.OperatingMode) error {
	return m.withDMSRecovery("SetOperatingMode", func(dms *qmi.DMSService) error {
		return dms.SetOperatingMode(ctx, mode)
	})
}

// WMSSendRawMessage 发送原始短信 PDU
func (m *Manager) WMSSendRawMessage(ctx context.Context, format uint8, pdu []byte) error {
	return m.withWMSRecovery("WMSSendRawMessage", func(wms *qmi.WMSService) error {
		return wms.SendRawMessage(ctx, format, pdu)
	})
}

// WMSRawReadMessage 读取原始短信 PDU
func (m *Manager) WMSRawReadMessage(ctx context.Context, storageType uint8, index uint32) ([]byte, error) {
	return withWMSRecoveryValue(m, "WMSRawReadMessage", func(wms *qmi.WMSService) ([]byte, error) {
		return wms.RawReadMessage(ctx, storageType, index)
	})
}

// WMSDeleteMessage 删除短信
func (m *Manager) WMSDeleteMessage(ctx context.Context, storageType uint8, index uint32) error {
	return m.withWMSRecovery("WMSDeleteMessage", func(wms *qmi.WMSService) error {
		return wms.DeleteMessage(ctx, storageType, index)
	})
}

// WMSListMessagesAuto 列出短信
func (m *Manager) WMSListMessagesAuto(ctx context.Context, storageType uint8) ([]struct {
	Index uint32
	Tag   qmi.MessageTagType
}, error) {
	return withWMSRecoveryValue(m, "WMSListMessagesAuto", func(wms *qmi.WMSService) ([]struct {
		Index uint32
		Tag   qmi.MessageTagType
	}, error) {
		return wms.ListMessagesAuto(ctx, storageType)
	})
}

// WMSDeleteMessagesByTag 按标签删除短信
func (m *Manager) WMSDeleteMessagesByTag(ctx context.Context, storageType uint8, tag qmi.MessageTagType, mode qmi.MessageMode) error {
	return m.withWMSRecovery("WMSDeleteMessagesByTag", func(wms *qmi.WMSService) error {
		return wms.DeleteMessagesByTag(ctx, storageType, tag, mode)
	})
}

// WMSRawWriteMessage 将短信写入模组存储并返回索引
func (m *Manager) WMSRawWriteMessage(ctx context.Context, storageType uint8, format uint8, pdu []byte) (uint32, error) {
	return withWMSRecoveryValue(m, "WMSRawWriteMessage", func(wms *qmi.WMSService) (uint32, error) {
		return wms.RawWriteMessage(ctx, storageType, format, pdu)
	})
}

// WMSGetMessageProtocol 获取当前短信协议
func (m *Manager) WMSGetMessageProtocol(ctx context.Context) (qmi.WMSMessageProtocol, error) {
	return withWMSRecoveryValue(m, "WMSGetMessageProtocol", func(wms *qmi.WMSService) (qmi.WMSMessageProtocol, error) {
		return wms.GetMessageProtocol(ctx)
	})
}

// WMSGetSupportedMessages 获取 WMS service 支持的消息 ID
func (m *Manager) WMSGetSupportedMessages(ctx context.Context) ([]uint8, error) {
	return withWMSRecoveryValue(m, "WMSGetSupportedMessages", func(wms *qmi.WMSService) ([]uint8, error) {
		return wms.GetSupportedMessages(ctx)
	})
}

// WMSSetRoutes 设置短信路由表
func (m *Manager) WMSSetRoutes(ctx context.Context, routes []qmi.WMSRoute, transferStatusReportToClient bool) error {
	return m.withWMSRecovery("WMSSetRoutes", func(wms *qmi.WMSService) error {
		return wms.SetRoutes(ctx, routes, transferStatusReportToClient)
	})
}

// WMSGetRoutes 获取短信路由表
func (m *Manager) WMSGetRoutes(ctx context.Context) (*qmi.WMSRouteConfig, error) {
	return withWMSRecoveryValue(m, "WMSGetRoutes", func(wms *qmi.WMSService) (*qmi.WMSRouteConfig, error) {
		return wms.GetRoutes(ctx)
	})
}

// WMSSendAck 发送短信 ACK
func (m *Manager) WMSSendAck(ctx context.Context, req qmi.WMSAckRequest) (*qmi.WMSAckResult, error) {
	return withWMSRecoveryValue(m, "WMSSendAck", func(wms *qmi.WMSService) (*qmi.WMSAckResult, error) {
		return wms.SendAck(ctx, req)
	})
}

// WMSSendFromStorage 从存储索引发送短信
func (m *Manager) WMSSendFromStorage(ctx context.Context, storageType uint8, index uint32, mode qmi.MessageMode, smsOnIMS bool) (*qmi.WMSSendFromStorageResult, error) {
	return withWMSRecoveryValue(m, "WMSSendFromStorage", func(wms *qmi.WMSService) (*qmi.WMSSendFromStorageResult, error) {
		return wms.SendFromStorage(ctx, storageType, index, mode, smsOnIMS)
	})
}

// WMSIndicationRegister 注册 WMS 指示上报开关
func (m *Manager) WMSIndicationRegister(ctx context.Context, reportTransportNetworkRegistration bool) error {
	return m.withWMSRecovery("WMSIndicationRegister", func(wms *qmi.WMSService) error {
		return wms.IndicationRegister(ctx, reportTransportNetworkRegistration)
	})
}

// WMSGetTransportNetworkRegistrationStatus 获取短信传输网络注册状态
func (m *Manager) WMSGetTransportNetworkRegistrationStatus(ctx context.Context) (qmi.WMSTransportNetworkRegistration, error) {
	return withWMSRecoveryValue(m, "WMSGetTransportNetworkRegistrationStatus", func(wms *qmi.WMSService) (qmi.WMSTransportNetworkRegistration, error) {
		return wms.GetTransportNetworkRegistrationStatus(ctx)
	})
}

// WDSGetChannelRates 获取当前/最大信道速率
func (m *Manager) WDSGetChannelRates(ctx context.Context) (*qmi.ChannelRates, error) {
	m.mu.RLock()
	wds := m.wds
	m.mu.RUnlock()
	if wds == nil {
		return nil, ErrServiceNotReady("WDS")
	}
	return wds.GetChannelRates(ctx)
}

// WDSGetPacketStatistics 获取 WDS 统计计数器
func (m *Manager) WDSGetPacketStatistics(ctx context.Context, mask uint32) (*qmi.PacketStatistics, error) {
	m.mu.RLock()
	wds := m.wds
	m.mu.RUnlock()
	if wds == nil {
		return nil, ErrServiceNotReady("WDS")
	}
	return wds.GetPacketStatistics(ctx, mask)
}

// WDSCreateProfile 创建 PDP Profile
func (m *Manager) WDSCreateProfile(ctx context.Context, profileType uint8, settings qmi.WDSProfileSettings) (*qmi.ProfileInfo, error) {
	m.mu.RLock()
	wds := m.wds
	m.mu.RUnlock()
	if wds == nil {
		return nil, ErrServiceNotReady("WDS")
	}
	return wds.CreateProfile(ctx, profileType, settings)
}

// WDSModifyProfileSettings 修改 PDP Profile
func (m *Manager) WDSModifyProfileSettings(ctx context.Context, profileType, profileIndex uint8, settings qmi.WDSProfileSettings) error {
	m.mu.RLock()
	wds := m.wds
	m.mu.RUnlock()
	if wds == nil {
		return ErrServiceNotReady("WDS")
	}
	return wds.ModifyProfileSettings(ctx, profileType, profileIndex, settings)
}

// WDSDeleteProfile 删除 PDP Profile
func (m *Manager) WDSDeleteProfile(ctx context.Context, profileType, profileIndex uint8) error {
	m.mu.RLock()
	wds := m.wds
	m.mu.RUnlock()
	if wds == nil {
		return ErrServiceNotReady("WDS")
	}
	return wds.DeleteProfile(ctx, profileType, profileIndex)
}

// WDSGetAutoconnectSettings 获取自动拨号设置
func (m *Manager) WDSGetAutoconnectSettings(ctx context.Context) (*qmi.AutoconnectSettings, error) {
	m.mu.RLock()
	wds := m.wds
	m.mu.RUnlock()
	if wds == nil {
		return nil, ErrServiceNotReady("WDS")
	}
	return wds.GetAutoconnectSettings(ctx)
}

// WDSSetAutoconnectSettings 设置自动拨号参数
func (m *Manager) WDSSetAutoconnectSettings(ctx context.Context, settings qmi.AutoconnectSettings) error {
	m.mu.RLock()
	wds := m.wds
	m.mu.RUnlock()
	if wds == nil {
		return ErrServiceNotReady("WDS")
	}
	return wds.SetAutoconnectSettings(ctx, settings)
}

// WDSGetDataBearerTechnology 获取传统承载制式信息
func (m *Manager) WDSGetDataBearerTechnology(ctx context.Context) (*qmi.DataBearerTechnologyInfo, error) {
	m.mu.RLock()
	wds := m.wds
	m.mu.RUnlock()
	if wds == nil {
		return nil, ErrServiceNotReady("WDS")
	}
	return wds.GetDataBearerTechnology(ctx)
}

// WDSGetCurrentDataBearerTechnology 获取当前承载网络/RAT/SO 信息
func (m *Manager) WDSGetCurrentDataBearerTechnology(ctx context.Context) (*qmi.CurrentBearerTechnologyInfo, error) {
	m.mu.RLock()
	wds := m.wds
	m.mu.RUnlock()
	if wds == nil {
		return nil, ErrServiceNotReady("WDS")
	}
	return wds.GetCurrentDataBearerTechnology(ctx)
}

// IMSABind 显式绑定 IMSA 到指定 subscription/binding
func (m *Manager) IMSABind(ctx context.Context, binding uint32) error {
	m.mu.RLock()
	imsa := m.imsa
	m.mu.RUnlock()
	if imsa == nil {
		return ErrServiceNotReady("IMSA")
	}
	return imsa.Bind(ctx, binding)
}

// IMSAGetIMSRegistrationStatus 获取 IMS 注册状态
func (m *Manager) IMSAGetIMSRegistrationStatus(ctx context.Context) (*qmi.IMSARegistrationStatus, error) {
	m.mu.RLock()
	imsa := m.imsa
	m.mu.RUnlock()
	if imsa == nil {
		return nil, ErrServiceNotReady("IMSA")
	}
	return imsa.GetIMSRegistrationStatus(ctx)
}

// IMSAGetIMSServicesStatus 获取 IMS 各业务可用状态
func (m *Manager) IMSAGetIMSServicesStatus(ctx context.Context) (*qmi.IMSAServicesStatus, error) {
	m.mu.RLock()
	imsa := m.imsa
	m.mu.RUnlock()
	if imsa == nil {
		return nil, ErrServiceNotReady("IMSA")
	}
	return imsa.GetIMSServicesStatus(ctx)
}

// IMSARegisterIndications 注册 IMSA 指示开关
func (m *Manager) IMSARegisterIndications(ctx context.Context, cfg qmi.IMSAIndicationRegistration) error {
	m.mu.RLock()
	imsa := m.imsa
	m.mu.RUnlock()
	if imsa == nil {
		return ErrServiceNotReady("IMSA")
	}
	return imsa.RegisterIndications(ctx, cfg)
}

// IMSBind 显式绑定 IMS settings service
func (m *Manager) IMSBind(ctx context.Context, binding uint32) error {
	m.mu.RLock()
	ims := m.ims
	m.mu.RUnlock()
	if ims == nil {
		return ErrServiceNotReady("IMS")
	}
	return ims.Bind(ctx, binding)
}

// IMSGetServicesEnabledSetting 获取 IMS enable setting
func (m *Manager) IMSGetServicesEnabledSetting(ctx context.Context) (*qmi.IMSServicesEnabledSettings, error) {
	m.mu.RLock()
	ims := m.ims
	m.mu.RUnlock()
	if ims == nil {
		return nil, ErrServiceNotReady("IMS")
	}
	return ims.GetServicesEnabledSetting(ctx)
}

// IMSSetServicesEnabledSetting 显式修改 IMS enable setting
func (m *Manager) IMSSetServicesEnabledSetting(ctx context.Context, update qmi.IMSServicesEnabledSettingsUpdate) error {
	m.mu.RLock()
	ims := m.ims
	m.mu.RUnlock()
	if ims == nil {
		return ErrServiceNotReady("IMS")
	}
	return ims.SetServicesEnabledSetting(ctx, update)
}

// IMSPGetEnablerState 获取 IMSP enabler 状态
func (m *Manager) IMSPGetEnablerState(ctx context.Context) (qmi.IMSPEnablerState, error) {
	m.mu.RLock()
	imsp := m.imsp
	m.mu.RUnlock()
	if imsp == nil {
		return 0, ErrServiceNotReady("IMSP")
	}
	return imsp.GetEnablerState(ctx)
}

// VOICEIndicationRegister 注册 VOICE 指示开关
func (m *Manager) VOICEIndicationRegister(ctx context.Context, cfg qmi.VoiceIndicationRegistration) error {
	return m.withVOICERecovery("VOICEIndicationRegister", func(voice *qmi.VOICEService) error {
		return voice.IndicationRegister(ctx, cfg)
	})
}

// VOICEGetSupportedMessages 获取 VOICE service 支持的消息 ID
func (m *Manager) VOICEGetSupportedMessages(ctx context.Context) ([]uint8, error) {
	return withVOICERecoveryValue(m, "VOICEGetSupportedMessages", func(voice *qmi.VOICEService) ([]uint8, error) {
		return voice.GetSupportedMessages(ctx)
	})
}

// VOICEDialCall 拨打语音电话
func (m *Manager) VOICEDialCall(ctx context.Context, number string) (uint8, error) {
	return withVOICERecoveryValue(m, "VOICEDialCall", func(voice *qmi.VOICEService) (uint8, error) {
		return voice.DialCall(ctx, number)
	})
}

// VOICEEndCall 挂断语音电话
func (m *Manager) VOICEEndCall(ctx context.Context, callID uint8) (uint8, error) {
	return withVOICERecoveryValue(m, "VOICEEndCall", func(voice *qmi.VOICEService) (uint8, error) {
		return voice.EndCall(ctx, callID)
	})
}

// VOICEAnswerCall 接听语音电话
func (m *Manager) VOICEAnswerCall(ctx context.Context, callID uint8) (uint8, error) {
	return withVOICERecoveryValue(m, "VOICEAnswerCall", func(voice *qmi.VOICEService) (uint8, error) {
		return voice.AnswerCall(ctx, callID)
	})
}

// VOICEBurstDTMF 发送一串 DTMF 按键
func (m *Manager) VOICEBurstDTMF(ctx context.Context, callID uint8, digits string) (uint8, error) {
	return withVOICERecoveryValue(m, "VOICEBurstDTMF", func(voice *qmi.VOICEService) (uint8, error) {
		return voice.BurstDTMF(ctx, callID, digits)
	})
}

// VOICEStartContinuousDTMF 开始持续 DTMF
func (m *Manager) VOICEStartContinuousDTMF(ctx context.Context, callID uint8, digit uint8) (uint8, error) {
	return withVOICERecoveryValue(m, "VOICEStartContinuousDTMF", func(voice *qmi.VOICEService) (uint8, error) {
		return voice.StartContinuousDTMF(ctx, callID, digit)
	})
}

// VOICEStopContinuousDTMF 停止持续 DTMF
func (m *Manager) VOICEStopContinuousDTMF(ctx context.Context, callID uint8) (uint8, error) {
	return withVOICERecoveryValue(m, "VOICEStopContinuousDTMF", func(voice *qmi.VOICEService) (uint8, error) {
		return voice.StopContinuousDTMF(ctx, callID)
	})
}

// VOICEGetAllCallInfo 获取当前全部通话信息
func (m *Manager) VOICEGetAllCallInfo(ctx context.Context) (*qmi.VoiceAllCallInfo, error) {
	return withVOICERecoveryValue(m, "VOICEGetAllCallInfo", func(voice *qmi.VOICEService) (*qmi.VoiceAllCallInfo, error) {
		return voice.GetAllCallInfo(ctx)
	})
}

// VOICEManageCalls 执行保持/恢复/切换等通话管理动作
func (m *Manager) VOICEManageCalls(ctx context.Context, req qmi.VoiceManageCallsRequest) error {
	return m.withVOICERecovery("VOICEManageCalls", func(voice *qmi.VOICEService) error {
		return voice.ManageCalls(ctx, req)
	})
}

// VOICESetSupplementaryService 设置补充业务
func (m *Manager) VOICESetSupplementaryService(ctx context.Context, req qmi.VoiceSupplementaryServiceRequest) (*qmi.VoiceSupplementaryServiceStatus, error) {
	return withVOICERecoveryValue(m, "VOICESetSupplementaryService", func(voice *qmi.VOICEService) (*qmi.VoiceSupplementaryServiceStatus, error) {
		return voice.SetSupplementaryService(ctx, req)
	})
}

// VOICEGetCallWaiting 查询呼叫等待状态
func (m *Manager) VOICEGetCallWaiting(ctx context.Context, serviceClass uint8) (uint8, error) {
	return withVOICERecoveryValue(m, "VOICEGetCallWaiting", func(voice *qmi.VOICEService) (uint8, error) {
		return voice.GetCallWaiting(ctx, serviceClass)
	})
}

// VOICEOriginateUSSD 发起 USSD
func (m *Manager) VOICEOriginateUSSD(ctx context.Context, req qmi.VoiceUSSDRequest) (*qmi.VoiceUSSDResponse, error) {
	return withVOICERecoveryValue(m, "VOICEOriginateUSSD", func(voice *qmi.VOICEService) (*qmi.VoiceUSSDResponse, error) {
		return voice.OriginateUSSD(ctx, req)
	})
}

// VOICEAnswerUSSD 回复 USSD
func (m *Manager) VOICEAnswerUSSD(ctx context.Context, req qmi.VoiceUSSDRequest) error {
	return m.withVOICERecovery("VOICEAnswerUSSD", func(voice *qmi.VOICEService) error {
		return voice.AnswerUSSD(ctx, req)
	})
}

// VOICECancelUSSD 取消当前 USSD 会话
func (m *Manager) VOICECancelUSSD(ctx context.Context) error {
	return m.withVOICERecovery("VOICECancelUSSD", func(voice *qmi.VOICEService) error {
		return voice.CancelUSSD(ctx)
	})
}

// VOICEGetConfig 查询语音配置
func (m *Manager) VOICEGetConfig(ctx context.Context, query qmi.VoiceConfigQuery) (*qmi.VoiceConfig, error) {
	return withVOICERecoveryValue(m, "VOICEGetConfig", func(voice *qmi.VOICEService) (*qmi.VoiceConfig, error) {
		return voice.GetConfig(ctx, query)
	})
}

// VOICEOriginateUSSDNoWait 发起异步 USSD
func (m *Manager) VOICEOriginateUSSDNoWait(ctx context.Context, req qmi.VoiceUSSDRequest) error {
	return m.withVOICERecovery("VOICEOriginateUSSDNoWait", func(voice *qmi.VOICEService) error {
		return voice.OriginateUSSDNoWait(ctx, req)
	})
}

// ErrServiceNotReady 返回指定 QMI 服务未初始化或未就绪的错误
func ErrServiceNotReady(service string) error {
	return &ServiceNotReadyError{Service: service}
}

// ServiceNotReadyError 表示 QMI 服务未就绪
type ServiceNotReadyError struct {
	Service string
}

func (e *ServiceNotReadyError) Error() string {
	return "QMI 服务未就绪: " + e.Service
}

// SMSNotReadyError 表示短信控制面尚未恢复就绪。
type SMSNotReadyError struct {
	TransportStatus      string
	TransportKnown       bool
	TransportUnsupported bool
	TransportQueryError  string
	SMSCAvailable        bool
	RoutesKnown          bool
	NASRegistered        *bool
}

func (e *SMSNotReadyError) Error() string {
	nasRegistered := "unknown"
	if e.NASRegistered != nil {
		nasRegistered = fmt.Sprintf("%t", *e.NASRegistered)
	}
	return fmt.Sprintf(
		"QMI 短信未就绪: transport_status=%s transport_known=%t transport_unsupported=%t smsc_available=%t routes_known=%t nas_registered=%s transport_query_error=%q",
		e.TransportStatus,
		e.TransportKnown,
		e.TransportUnsupported,
		e.SMSCAvailable,
		e.RoutesKnown,
		nasRegistered,
		e.TransportQueryError,
	)
}

func (m *Manager) NASPerformNetworkScan(ctx context.Context) ([]qmi.NetworkScanResult, error) {
	return withNASRecoveryValue(m, "NASPerformNetworkScan", func(nas *qmi.NASService) ([]qmi.NetworkScanResult, error) {
		return nas.PerformNetworkScan(ctx)
	})
}
