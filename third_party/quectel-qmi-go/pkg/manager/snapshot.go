package manager

import (
	"sync"
	"time"

	"github.com/iniwex5/quectel-qmi-go/pkg/qmi"
)

// ============================================================================
// DeviceSnapshot — 设备状态快照，由 NAS Indication 事件驱动更新
// ============================================================================
//
//
// 缓存的种类：
// 1. 完全动态：ServingSystem, Signal 等 (依赖 Indication / Polling)
// 2. 静态及半静态：IMEI, IMSI, ICCID, 固件版本等 (由 Manager 的拉取逻辑预热写入)
//
// 上层可通过 Manager.GetDeviceSnapshot() 零 IPC 读取。

// DeviceIdentities 设备的核心不变与半固化标识快照（如 SIM 信息）。
type DeviceIdentities struct {
	IMEI             string
	ICCID            string
	IMSI             string
	FirmwareRevision string
	HardwareRevision string
	Manufacturer     string
	Model            string
	OperatingMode    *qmi.OperatingMode
	SimInserted      *bool
}

// DeviceSnapshot 记录由 QMI Indication 事件驱动的最新设备网络状态。
type DeviceSnapshot struct {
	mu sync.RWMutex

	// 来自 NAS ServingSystemChanged indication
	servingSystem *qmi.ServingSystem
	lastServing   time.Time

	// 来自 SignalUpdate（doStatusCheck 或 Indication 均会触发）
	signal     *qmi.SignalStrength
	lastSignal time.Time

	// 来自 NASSysInfoInd (0 IPC 获取网络小区状态)
	sysInfo     *qmi.SysInfo
	lastSysInfo time.Time

	// 来自 NAS OperatorName indication
	nasOperatorName      *qmi.NASOperatorNameInfo
	lastNASOperatorName  time.Time
	nasOperatorNameValid bool

	// 来自 NAS NetworkTime indication
	nasNetworkTime      *qmi.NetworkTimeInfo
	lastNASNetworkTime  time.Time
	nasNetworkTimeValid bool

	// 来自 NAS SignalInfo indication
	nasSignalInfo      *qmi.SignalInfo
	lastNASSignalInfo  time.Time
	nasSignalInfoValid bool

	// 来自 NAS NetworkReject indication（最近一次）
	nasNetworkReject      *qmi.NASNetworkRejectInfo
	lastNASNetworkReject  time.Time
	nasNetworkRejectValid bool

	// 来自 NAS IncrementalNetworkScan indication（最近一次任务状态）
	nasIncrementalScan      *qmi.NASIncrementalNetworkScanInfo
	lastNASIncrementalScan  time.Time
	nasIncrementalScanValid bool

	// 来自 UIM refresh indication
	uimRefresh      *qmi.UIMRefreshIndication
	lastUIMRefresh  time.Time
	uimRefreshValid bool

	// 来自 UIM slot status indication
	uimSlotStatus      *qmi.UIMSlotStatus
	lastUIMSlotStatus  time.Time
	uimSlotStatusValid bool

	// 来自内部的 PreWarm 和刷新操作组
	identities            DeviceIdentities
	identitiesStaticReady bool
	identitiesSIMReady    bool
	identitiesGeneration  uint64
}

func cloneOperatingMode(in *qmi.OperatingMode) *qmi.OperatingMode {
	if in == nil {
		return nil
	}
	v := *in
	return &v
}

func cloneBool(in *bool) *bool {
	if in == nil {
		return nil
	}
	v := *in
	return &v
}

func cloneIdentities(in DeviceIdentities) DeviceIdentities {
	out := in
	out.OperatingMode = cloneOperatingMode(in.OperatingMode)
	out.SimInserted = cloneBool(in.SimInserted)
	return out
}

func cloneServingSystem(in *qmi.ServingSystem) *qmi.ServingSystem {
	if in == nil {
		return nil
	}
	v := *in
	return &v
}

func cloneSignalStrength(in *qmi.SignalStrength) *qmi.SignalStrength {
	if in == nil {
		return nil
	}
	v := *in
	return &v
}

func cloneSysInfo(in *qmi.SysInfo) *qmi.SysInfo {
	if in == nil {
		return nil
	}
	v := *in
	return &v
}

func cloneNASOperatorName(in *qmi.NASOperatorNameInfo) *qmi.NASOperatorNameInfo {
	if in == nil {
		return nil
	}
	v := *in
	return &v
}

func cloneNASNetworkTime(in *qmi.NetworkTimeInfo) *qmi.NetworkTimeInfo {
	if in == nil {
		return nil
	}
	v := *in
	return &v
}

func cloneNASSignalInfo(in *qmi.SignalInfo) *qmi.SignalInfo {
	if in == nil {
		return nil
	}
	v := *in
	return &v
}

func cloneNASNetworkReject(in *qmi.NASNetworkRejectInfo) *qmi.NASNetworkRejectInfo {
	if in == nil {
		return nil
	}
	v := *in
	return &v
}

func cloneNASIncrementalScan(in *qmi.NASIncrementalNetworkScanInfo) *qmi.NASIncrementalNetworkScanInfo {
	if in == nil {
		return nil
	}
	return &qmi.NASIncrementalNetworkScanInfo{
		ScanComplete: in.ScanComplete,
		Results:      cloneScanResults(in.Results),
	}
}

func cloneSnapshotUIMRefreshFiles(in []qmi.UIMRefreshFile) []qmi.UIMRefreshFile {
	if len(in) == 0 {
		return nil
	}
	out := make([]qmi.UIMRefreshFile, len(in))
	for i := range in {
		out[i] = in[i]
		if len(in[i].Path) > 0 {
			out[i].Path = append([]byte(nil), in[i].Path...)
		}
	}
	return out
}

func cloneSnapshotUIMRefreshIndication(in *qmi.UIMRefreshIndication) *qmi.UIMRefreshIndication {
	if in == nil {
		return nil
	}
	out := *in
	out.ApplicationIdentifier = append([]byte(nil), in.ApplicationIdentifier...)
	out.Files = cloneSnapshotUIMRefreshFiles(in.Files)
	return &out
}

func cloneSnapshotUIMSlotStatus(in *qmi.UIMSlotStatus) *qmi.UIMSlotStatus {
	if in == nil {
		return nil
	}
	out := *in
	if len(in.Slots) > 0 {
		out.Slots = make([]qmi.UIMSlotStatusSlot, len(in.Slots))
		for i := range in.Slots {
			out.Slots[i] = in.Slots[i]
			out.Slots[i].ICCIDRaw = append([]byte(nil), in.Slots[i].ICCIDRaw...)
			out.Slots[i].ATR = append([]byte(nil), in.Slots[i].ATR...)
			out.Slots[i].EID = append([]byte(nil), in.Slots[i].EID...)
		}
	}
	return &out
}

// updateServingRegistration 仅更新 ServingSystem 中注册态相关字段。
func (s *DeviceSnapshot) updateServingRegistration(ss *qmi.ServingSystem) {
	if ss == nil {
		return
	}
	s.mu.Lock()
	if s.servingSystem == nil {
		s.servingSystem = &qmi.ServingSystem{}
	}
	s.servingSystem.RegistrationState = ss.RegistrationState
	s.servingSystem.PSAttached = ss.PSAttached
	s.servingSystem.RadioInterface = ss.RadioInterface
	s.lastServing = time.Now()
	s.mu.Unlock()
}

// updateServingFromQuery 将 NAS GetServingSystem 主动查询结果按 merge 语义回填快照。
// 注册态字段总是更新，PLMN 仅在非零时更新，防止短窗口把已知 PLMN 清空。
func (s *DeviceSnapshot) updateServingFromQuery(ss *qmi.ServingSystem) {
	if ss == nil {
		return
	}
	s.mu.Lock()
	if s.servingSystem == nil {
		s.servingSystem = &qmi.ServingSystem{}
	}
	s.servingSystem.RegistrationState = ss.RegistrationState
	s.servingSystem.PSAttached = ss.PSAttached
	s.servingSystem.RadioInterface = ss.RadioInterface
	if ss.MCC != 0 || ss.MNC != 0 {
		s.servingSystem.MCC = ss.MCC
		s.servingSystem.MNC = ss.MNC
	}
	s.lastServing = time.Now()
	s.mu.Unlock()
}

func (s *DeviceSnapshot) updateServingPLMN(mcc, mnc uint16) {
	if mcc == 0 && mnc == 0 {
		return
	}
	s.mu.Lock()
	if s.servingSystem == nil {
		s.servingSystem = &qmi.ServingSystem{
			RegistrationState: qmi.RegStateUnknown,
			MCC:               mcc,
			MNC:               mnc,
		}
	} else {
		s.servingSystem.MCC = mcc
		s.servingSystem.MNC = mnc
	}
	s.lastServing = time.Now()
	s.mu.Unlock()
}

func (s *DeviceSnapshot) updateSysInfo(si *qmi.SysInfo) {
	if si == nil {
		return
	}
	copied := *si
	s.mu.Lock()
	s.sysInfo = &copied
	s.lastSysInfo = time.Now()
	s.mu.Unlock()
}

func (s *DeviceSnapshot) updateNASOperatorName(info *qmi.NASOperatorNameInfo) {
	if info == nil {
		return
	}
	copied := *info
	s.mu.Lock()
	s.nasOperatorName = &copied
	s.lastNASOperatorName = time.Now()
	s.nasOperatorNameValid = true
	s.mu.Unlock()
}

func (s *DeviceSnapshot) updateNASNetworkTime(info *qmi.NetworkTimeInfo) {
	if info == nil {
		return
	}
	copied := *info
	s.mu.Lock()
	s.nasNetworkTime = &copied
	s.lastNASNetworkTime = time.Now()
	s.nasNetworkTimeValid = true
	s.mu.Unlock()
}

func (s *DeviceSnapshot) updateNASSignalInfo(info *qmi.SignalInfo) {
	if info == nil {
		return
	}
	copied := *info
	s.mu.Lock()
	s.nasSignalInfo = &copied
	s.lastNASSignalInfo = time.Now()
	s.nasSignalInfoValid = true
	s.mu.Unlock()
}

func (s *DeviceSnapshot) updateNASNetworkReject(info *qmi.NASNetworkRejectInfo) {
	if info == nil {
		return
	}
	copied := *info
	s.mu.Lock()
	s.nasNetworkReject = &copied
	s.lastNASNetworkReject = time.Now()
	s.nasNetworkRejectValid = true
	s.mu.Unlock()
}

func cloneScanResults(in []qmi.NetworkScanResult) []qmi.NetworkScanResult {
	if len(in) == 0 {
		return nil
	}
	out := make([]qmi.NetworkScanResult, len(in))
	for i := range in {
		out[i] = in[i]
		if len(in[i].RATs) > 0 {
			out[i].RATs = append([]uint8(nil), in[i].RATs...)
		}
	}
	return out
}

func mergeScanResults(oldResults, newResults []qmi.NetworkScanResult) []qmi.NetworkScanResult {
	if len(oldResults) == 0 {
		return cloneScanResults(newResults)
	}
	if len(newResults) == 0 {
		return cloneScanResults(oldResults)
	}

	merged := cloneScanResults(oldResults)
	indexByKey := make(map[string]int, len(merged))
	for i := range merged {
		key := merged[i].MCC + "|" + merged[i].MNC + "|" + merged[i].Description
		indexByKey[key] = i
	}

	for i := range newResults {
		entry := newResults[i]
		key := entry.MCC + "|" + entry.MNC + "|" + entry.Description
		copied := entry
		if len(entry.RATs) > 0 {
			copied.RATs = append([]uint8(nil), entry.RATs...)
		}
		if idx, ok := indexByKey[key]; ok {
			merged[idx] = copied
			continue
		}
		indexByKey[key] = len(merged)
		merged = append(merged, copied)
	}
	return merged
}

func (s *DeviceSnapshot) updateNASIncrementalScan(info *qmi.NASIncrementalNetworkScanInfo) {
	if info == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	var mergedResults []qmi.NetworkScanResult
	if s.nasIncrementalScan != nil {
		mergedResults = mergeScanResults(s.nasIncrementalScan.Results, info.Results)
	} else {
		mergedResults = cloneScanResults(info.Results)
	}

	copied := &qmi.NASIncrementalNetworkScanInfo{
		ScanComplete: info.ScanComplete,
		Results:      mergedResults,
	}
	s.nasIncrementalScan = copied
	s.lastNASIncrementalScan = time.Now()
	s.nasIncrementalScanValid = true
}

func (s *DeviceSnapshot) updateUIMRefresh(info *qmi.UIMRefreshIndication) {
	if info == nil {
		return
	}
	s.mu.Lock()
	s.uimRefresh = cloneSnapshotUIMRefreshIndication(info)
	s.lastUIMRefresh = time.Now()
	s.uimRefreshValid = true
	s.mu.Unlock()
}

func (s *DeviceSnapshot) updateUIMSlotStatus(info *qmi.UIMSlotStatus) {
	if info == nil {
		return
	}
	s.mu.Lock()
	s.uimSlotStatus = cloneSnapshotUIMSlotStatus(info)
	s.lastUIMSlotStatus = time.Now()
	s.uimSlotStatusValid = true
	s.mu.Unlock()
}

// updateSignal 由 emitSignalUpdate 时同步调用。
// 内部加锁，调用方无需额外同步。
func (s *DeviceSnapshot) updateSignal(sig *qmi.SignalStrength) {
	if sig == nil {
		return
	}
	copied := *sig
	s.mu.Lock()
	s.signal = &copied
	s.lastSignal = time.Now()
	s.mu.Unlock()
}

// ServingSystem 返回最新的服务系统快照及其时间戳。
// 如果从未更新过，返回 nil 和 zero time。
func (s *DeviceSnapshot) ServingSystem() (*qmi.ServingSystem, time.Time) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneServingSystem(s.servingSystem), s.lastServing
}

// Signal 返回最新的信号强度快照及其时间戳。
// 如果从未更新过，返回 nil 和 zero time。
func (s *DeviceSnapshot) Signal() (*qmi.SignalStrength, time.Time) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneSignalStrength(s.signal), s.lastSignal
}

// SysInfo 返回最新的小区系统信息及时间戳。
func (s *DeviceSnapshot) SysInfo() (*qmi.SysInfo, time.Time) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneSysInfo(s.sysInfo), s.lastSysInfo
}

// NASOperatorName 返回最新 NAS 运营商名称及时间戳和有效标记。
func (s *DeviceSnapshot) NASOperatorName() (*qmi.NASOperatorNameInfo, time.Time, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneNASOperatorName(s.nasOperatorName), s.lastNASOperatorName, s.nasOperatorNameValid
}

// NASNetworkTime 返回最新 NAS 网络时间及时间戳和有效标记。
func (s *DeviceSnapshot) NASNetworkTime() (*qmi.NetworkTimeInfo, time.Time, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneNASNetworkTime(s.nasNetworkTime), s.lastNASNetworkTime, s.nasNetworkTimeValid
}

// NASSignalInfo 返回最新 NAS 信号信息及时间戳和有效标记。
func (s *DeviceSnapshot) NASSignalInfo() (*qmi.SignalInfo, time.Time, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneNASSignalInfo(s.nasSignalInfo), s.lastNASSignalInfo, s.nasSignalInfoValid
}

// NASNetworkReject 返回最近一次 NAS 驻网拒绝信息及时间戳和有效标记。
func (s *DeviceSnapshot) NASNetworkReject() (*qmi.NASNetworkRejectInfo, time.Time, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneNASNetworkReject(s.nasNetworkReject), s.lastNASNetworkReject, s.nasNetworkRejectValid
}

// NASIncrementalScan 返回最近一次 NAS 增量搜网状态及时间戳和有效标记。
func (s *DeviceSnapshot) NASIncrementalScan() (*qmi.NASIncrementalNetworkScanInfo, time.Time, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneNASIncrementalScan(s.nasIncrementalScan), s.lastNASIncrementalScan, s.nasIncrementalScanValid
}

// UIMRefresh 返回最近一次 UIM refresh indication 及时间戳和有效标记。
func (s *DeviceSnapshot) UIMRefresh() (*qmi.UIMRefreshIndication, time.Time, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneSnapshotUIMRefreshIndication(s.uimRefresh), s.lastUIMRefresh, s.uimRefreshValid
}

// UIMSlotStatus 返回最近一次 UIM slot status indication 及时间戳和有效标记。
func (s *DeviceSnapshot) UIMSlotStatus() (*qmi.UIMSlotStatus, time.Time, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneSnapshotUIMSlotStatus(s.uimSlotStatus), s.lastUIMSlotStatus, s.uimSlotStatusValid
}

// UpdateIdentities 由 Manager 组件异步拉取汇总后同步调用写入。
func (s *DeviceSnapshot) UpdateIdentities(ids DeviceIdentities) {
	s.mu.Lock()
	s.updateIdentitiesLocked(ids)
	s.mu.Unlock()
}

func (s *DeviceSnapshot) updateIdentitiesLocked(ids DeviceIdentities) {
	if ids.IMEI != "" {
		s.identities.IMEI = ids.IMEI
	}
	if ids.ICCID != "" {
		s.identities.ICCID = ids.ICCID
	}
	if ids.IMSI != "" {
		s.identities.IMSI = ids.IMSI
	}
	if ids.FirmwareRevision != "" {
		s.identities.FirmwareRevision = ids.FirmwareRevision
	}
	if ids.HardwareRevision != "" {
		s.identities.HardwareRevision = ids.HardwareRevision
	}
	if ids.Manufacturer != "" {
		s.identities.Manufacturer = ids.Manufacturer
	}
	if ids.Model != "" {
		s.identities.Model = ids.Model
	}
	if ids.OperatingMode != nil {
		s.identities.OperatingMode = cloneOperatingMode(ids.OperatingMode)
	}
	if ids.SimInserted != nil {
		s.identities.SimInserted = cloneBool(ids.SimInserted)
	}

	hasStatic := s.identities.IMEI != "" || s.identities.FirmwareRevision != "" || s.identities.HardwareRevision != "" || s.identities.Manufacturer != "" || s.identities.Model != "" || s.identities.OperatingMode != nil
	hasSIM := s.identities.ICCID != "" || s.identities.IMSI != "" || s.identities.SimInserted != nil
	s.identitiesStaticReady = hasStatic
	s.identitiesSIMReady = hasSIM
}

func (s *DeviceSnapshot) UpdateIdentitiesIfGeneration(ids DeviceIdentities, generation uint64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if generation != s.identitiesGeneration {
		return false
	}
	s.updateIdentitiesLocked(ids)
	return true
}

// ResetIdentities 用于清除会随 SIM 卡变化的标识数据缓存（ICCID / IMSI），
// 或者在明确丢失底层数据时使用。对于 IMEI 坚固数据可以酌情保留。
func (s *DeviceSnapshot) ResetIdentities(clearAll bool) {
	s.mu.Lock()
	s.identitiesGeneration++
	if clearAll {
		s.identities = DeviceIdentities{}
		s.identitiesStaticReady = false
		s.identitiesSIMReady = false
	} else {
		// 仅清空卡强相关字段
		s.identities.ICCID = ""
		s.identities.IMSI = ""
		s.identities.SimInserted = nil
		s.identitiesSIMReady = false
	}
	s.mu.Unlock()
}

// Identities 返回设备标识缓存字典与当前是否可用的就绪状态。
func (s *DeviceSnapshot) Identities() (DeviceIdentities, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneIdentities(s.identities), s.identitiesStaticReady
}

func (s *DeviceSnapshot) IdentityReadiness() (bool, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.identitiesStaticReady, s.identitiesSIMReady
}

func (s *DeviceSnapshot) IdentityGeneration() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.identitiesGeneration
}

// Reset 清空所有动态与强相关快照数据。
// 在 Modem Reset 时由上层调用，确保不会读到旧数据。
func (s *DeviceSnapshot) Reset() {
	s.mu.Lock()
	s.servingSystem = nil
	s.lastServing = time.Time{}
	s.signal = nil
	s.lastSignal = time.Time{}
	s.sysInfo = nil
	s.lastSysInfo = time.Time{}
	s.nasOperatorName = nil
	s.lastNASOperatorName = time.Time{}
	s.nasOperatorNameValid = false
	s.nasNetworkTime = nil
	s.lastNASNetworkTime = time.Time{}
	s.nasNetworkTimeValid = false
	s.nasSignalInfo = nil
	s.lastNASSignalInfo = time.Time{}
	s.nasSignalInfoValid = false
	s.nasNetworkReject = nil
	s.lastNASNetworkReject = time.Time{}
	s.nasNetworkRejectValid = false
	s.nasIncrementalScan = nil
	s.lastNASIncrementalScan = time.Time{}
	s.nasIncrementalScanValid = false
	s.uimRefresh = nil
	s.lastUIMRefresh = time.Time{}
	s.uimRefreshValid = false
	s.uimSlotStatus = nil
	s.lastUIMSlotStatus = time.Time{}
	s.uimSlotStatusValid = false
	// 清空卡关连信息，但可保留硬件坚固信息
	s.identities.ICCID = ""
	s.identities.IMSI = ""
	s.identities.SimInserted = nil
	s.identitiesSIMReady = false
	s.identitiesGeneration++
	s.mu.Unlock()
}

// GetDeviceSnapshot 返回当前设备状态快照的指针。
// 调用方可通过 ServingSystem() 和 Signal() 方法分别读取。
// 该方法永远不会阻塞，不发出任何 QMI IPC。
func (m *Manager) GetDeviceSnapshot() *DeviceSnapshot {
	return &m.snapshot
}
