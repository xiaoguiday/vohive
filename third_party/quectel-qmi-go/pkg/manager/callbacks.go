package manager

import (
	"net"
	"sync"
	"sync/atomic"

	"github.com/iniwex5/quectel-qmi-go/pkg/qmi"
)

// ============================================================================
// State Callbacks / 状态回调
// 允许调用方监听连接状态变化
// ============================================================================

// EventType represents the type of connection event / EventType 表示连接事件类型
type EventType int

const (
	EventConnected                             EventType = iota // Connection established / 连接已建立
	EventDisconnected                                           // Connection lost / 连接丢失
	EventIPChanged                                              // IP address changed / IP 地址变化
	EventSignalUpdate                                           // Signal strength updated / 信号强度更新
	EventDialFailed                                             // Dial attempt failed / 拨号失败
	EventReconnecting                                           // Starting reconnection / 开始重连
	EventNewSMS                                                 // New SMS received (storage index) / 收到新短信（存储索引）
	EventNewSMSRaw                                              // New SMS received raw / 收到新的原始短消息 (直投)
	EventIMSRegistrationStatus                                  // IMS registration status changed / IMS 注册状态变化
	EventIMSServicesStatus                                      // IMS services status changed / IMS 业务状态变化
	EventIMSSettingsChanged                                     // IMS settings changed / IMS 配置状态变化
	EventVoiceCallStatus                                        // Voice call status indication / 语音通话状态指示
	EventVoiceUSSD                                              // Voice USSD indication / 语音 USSD 指示
	EventVoiceUSSDReleased                                      // Voice USSD released indication / 语音 USSD 释放指示
	EventVoiceSupplementaryService                              // Voice supplementary service indication / 语音补充业务指示
	EventVoiceUSSDNoWaitResult                                  // Voice originate USSD no wait result / 语音异步 USSD 结果
	EventWMSSMSCAddress                                         // WMS SMSC address indication / WMS 短信中心地址指示
	EventWMSTransportNetworkRegistrationStatus                  // WMS transport network registration status indication / WMS 传输网络注册状态指示
	EventPacketServiceStatusChanged                             // Packet service status changed indication / 数据服务状态改变指示
	EventServingSystemChanged                                   // Serving system changed indication / 服务系统改变指示
	EventNASOperatorNameChanged                                 // NAS operator name changed indication / NAS 运营商名称变化指示
	EventNASNetworkTimeChanged                                  // NAS network time changed indication / NAS 网络时间变化指示
	EventNASSignalInfoChanged                                   // NAS signal info changed indication / NAS 信号信息变化指示
	EventNASNetworkReject                                       // NAS network reject indication / NAS 驻网拒绝指示
	EventNASIncrementalNetworkScan                              // NAS incremental network scan indication / NAS 增量搜网指示
	EventModemReset                                             // Modem reset indication / Modem 重置指示
	EventSimStatusChanged                                       // SIM status changed indication / SIM 状态改变指示
	EventUIMSessionClosed                                       // UIM session closed indication / UIM 会话关闭指示
	EventUIMRefresh                                             // UIM refresh indication / UIM 刷新指示
	EventUIMSlotStatus                                          // UIM slot status indication / UIM 卡槽状态指示
	EventNASEventReport                                         // NAS event report / NAS 事件报告
	EventUnknownIndication                                      // Unknown indication / 未知指示
	EventVoiceSupplementaryServiceRequest                       // Voice supplementary service request indication / 语音补充业务请求指示
	EventRecoveryExhausted                                      // qmi-go 内部核心恢复已彻底放弃 / core recovery abandoned
)

func (e EventType) String() string {
	switch e {
	case EventConnected:
		return "Connected"
	case EventDisconnected:
		return "Disconnected"
	case EventIPChanged:
		return "IPChanged"
	case EventSignalUpdate:
		return "SignalUpdate"
	case EventDialFailed:
		return "DialFailed"
	case EventReconnecting:
		return "Reconnecting"
	case EventNewSMS:
		return "NewSMS"
	case EventNewSMSRaw:
		return "NewSMSRaw"
	case EventIMSRegistrationStatus:
		return "IMSRegistrationStatus"
	case EventIMSServicesStatus:
		return "IMSServicesStatus"
	case EventIMSSettingsChanged:
		return "IMSSettingsChanged"
	case EventVoiceCallStatus:
		return "VoiceCallStatus"
	case EventVoiceUSSD:
		return "VoiceUSSD"
	case EventVoiceUSSDReleased:
		return "VoiceUSSDReleased"
	case EventVoiceSupplementaryService:
		return "VoiceSupplementaryService"
	case EventVoiceSupplementaryServiceRequest:
		return "VoiceSupplementaryServiceRequest"
	case EventVoiceUSSDNoWaitResult:
		return "VoiceUSSDNoWaitResult"
	case EventWMSSMSCAddress:
		return "WMSSMSCAddress"
	case EventWMSTransportNetworkRegistrationStatus:
		return "WMSTransportNetworkRegistrationStatus"
	case EventPacketServiceStatusChanged:
		return "PacketServiceStatusChanged"
	case EventServingSystemChanged:
		return "ServingSystemChanged"
	case EventNASOperatorNameChanged:
		return "NASOperatorNameChanged"
	case EventNASNetworkTimeChanged:
		return "NASNetworkTimeChanged"
	case EventNASSignalInfoChanged:
		return "NASSignalInfoChanged"
	case EventNASNetworkReject:
		return "NASNetworkReject"
	case EventNASIncrementalNetworkScan:
		return "NASIncrementalNetworkScan"
	case EventNASEventReport:
		return "NASEventReport"
	case EventModemReset:
		return "ModemReset"
	case EventSimStatusChanged:
		return "SimStatusChanged"
	case EventUIMSessionClosed:
		return "UIMSessionClosed"
	case EventUIMRefresh:
		return "UIMRefresh"
	case EventUIMSlotStatus:
		return "UIMSlotStatus"
	case EventUnknownIndication:
		return "UnknownIndication"
	case EventRecoveryExhausted:
		return "RecoveryExhausted"
	default:
		return "Unknown"
	}
}

// Event represents a connection event / Event 表示连接事件
type Event struct {
	Type                      EventType                                       // Event type / 事件类型
	State                     State                                           // Current state / 当前状态
	Settings                  *qmi.RuntimeSettings                            // IP settings (for Connected/IPChanged) / IP 设置
	Error                     error                                           // Error (for DialFailed) / 错误信息
	Signal                    *qmi.SignalStrength                             // Signal info (for SignalUpdate) / 信号信息
	SMSIndex                  uint32                                          // SMS index (for NewSMS) / 短信索引
	StorageType               uint8                                           // SMS storage type (for NewSMS) / 短信存储类型
	Pdu                       []byte                                          // SMS Raw Data PDU (for EventNewSMSRaw) / 短信原始 PDU 数据
	SMSAckRequired            bool                                            // Raw SMS requires WMS ack / 原始短信需要 WMS ACK
	SMSTransactionID          uint32                                          // Raw SMS transaction ID for WMS ack / 原始短信 ACK 事务 ID
	SMSFormat                 uint8                                           // Raw SMS format / 原始短信格式
	IMSRegistration           *qmi.IMSARegistrationStatus                     // IMS registration status / IMS 注册状态
	IMSServices               *qmi.IMSAServicesStatus                         // IMS services status / IMS 业务状态
	IMSSettings               *qmi.IMSServicesEnabledSettings                 // IMS enabled settings / IMS 配置状态
	VoiceCalls                *qmi.VoiceAllCallInfo                           // Voice call status / 语音通话状态
	VoiceUSSD                 *qmi.VoiceUSSDIndication                        // Voice USSD / 语音 USSD
	VoiceSupplementary        *qmi.VoiceSupplementaryServiceIndication        // Voice supplementary service / 语音补充业务
	VoiceSupplementaryRequest *qmi.VoiceSupplementaryServiceRequestIndication // Voice supplementary service request / 语音补充业务请求
	VoiceUSSDNoWait           *qmi.VoiceUSSDNoWaitIndication                  // Voice async USSD result / 异步 USSD 结果
	ServingSystem             *qmi.ServingSystem                              // NAS serving system / NAS 服务系统
	NASOperatorName           *qmi.NASOperatorNameInfo                        // NAS operator name / NAS 运营商名称
	NASNetworkTime            *qmi.NetworkTimeInfo                            // NAS network time / NAS 网络时间
	NASSignalInfo             *qmi.SignalInfo                                 // NAS signal info / NAS 信号详情
	NASNetworkReject          *qmi.NASNetworkRejectInfo                       // NAS network reject / NAS 驻网拒绝
	NASIncrementalNetwork     *qmi.NASIncrementalNetworkScanInfo              // NAS incremental scan / NAS 增量搜网
	PacketServiceStatus       qmi.ConnectionStatus                            // WDS packet service status / WDS 数据服务状态
	UIMRefresh                *qmi.UIMRefreshIndication                       // UIM refresh indication payload / UIM 刷新指示载荷
	UIMSlotStatus             *qmi.UIMSlotStatus                              // UIM slot status indication payload / UIM 卡槽状态指示载荷
	WMSSMSCAddress            *qmi.WMSSMSCAddress                             // WMS SMSC address / WMS 短信中心地址
	WMSTransportRegistration  qmi.WMSTransportNetworkRegistration             // WMS transport registration / WMS 传输网络注册状态
	TLVMeta                   []qmi.TLVMeta                                   // TLV metadata for diagnostics / TLV 元数据（诊断用）
	RawQMIType                qmi.EventType                                   // Raw QMI event type / 原始 QMI 事件类型
	ServiceID                 uint8                                           // QMI service id / QMI 服务 ID
	MessageID                 uint16                                          // QMI message id / QMI 消息 ID
	Reason                    string                                          // 终态/诊断原因，如 device_removed / recovery_exhausted
}

// EventHandler is a callback function for connection events / EventHandler 是连接事件的回调函数
type EventHandler func(event Event)

// EventEmitter manages event handlers / EventEmitter 管理事件处理器
type EventEmitter struct {
	mu        sync.RWMutex
	handlers  []EventHandler
	queue     chan Event
	done      chan struct{}
	closeOnce sync.Once
	dropped   atomic.Uint64
}

// NewEventEmitter creates a new event emitter / NewEventEmitter 创建新的事件发射器
func NewEventEmitter() *EventEmitter {
	return NewEventEmitterWithQueueSize(128)
}

func NewEventEmitterWithQueueSize(size int) *EventEmitter {
	if size <= 0 {
		size = 128
	}
	e := &EventEmitter{
		handlers: make([]EventHandler, 0),
		queue:    make(chan Event, size),
		done:     make(chan struct{}),
	}
	go e.loop()
	return e
}

func cloneBytes(in []byte) []byte {
	if len(in) == 0 {
		return nil
	}
	out := make([]byte, len(in))
	copy(out, in)
	return out
}

func cloneUint16s(in []uint16) []uint16 {
	if len(in) == 0 {
		return nil
	}
	out := make([]uint16, len(in))
	copy(out, in)
	return out
}

func cloneIP(in net.IP) net.IP {
	if len(in) == 0 {
		return nil
	}
	out := make(net.IP, len(in))
	copy(out, in)
	return out
}

func cloneIPMask(in net.IPMask) net.IPMask {
	if len(in) == 0 {
		return nil
	}
	out := make(net.IPMask, len(in))
	copy(out, in)
	return out
}

func cloneRuntimeSettings(in *qmi.RuntimeSettings) *qmi.RuntimeSettings {
	if in == nil {
		return nil
	}
	out := *in
	out.IPv4Address = cloneIP(in.IPv4Address)
	out.IPv4Subnet = cloneIPMask(in.IPv4Subnet)
	out.IPv4Gateway = cloneIP(in.IPv4Gateway)
	out.IPv4DNS1 = cloneIP(in.IPv4DNS1)
	out.IPv4DNS2 = cloneIP(in.IPv4DNS2)
	out.IPv6Address = cloneIP(in.IPv6Address)
	out.IPv6Gateway = cloneIP(in.IPv6Gateway)
	out.IPv6DNS1 = cloneIP(in.IPv6DNS1)
	out.IPv6DNS2 = cloneIP(in.IPv6DNS2)
	return &out
}

func cloneSignalStrengthForEvent(in *qmi.SignalStrength) *qmi.SignalStrength {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}

func cloneIMSARegistrationStatus(in *qmi.IMSARegistrationStatus) *qmi.IMSARegistrationStatus {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}

func cloneIMSAServicesStatus(in *qmi.IMSAServicesStatus) *qmi.IMSAServicesStatus {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}

func cloneIMSServicesEnabledSettings(in *qmi.IMSServicesEnabledSettings) *qmi.IMSServicesEnabledSettings {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}

func cloneVoiceAllCallInfo(in *qmi.VoiceAllCallInfo) *qmi.VoiceAllCallInfo {
	if in == nil {
		return nil
	}
	out := *in
	if len(in.Calls) > 0 {
		out.Calls = append([]qmi.VoiceCallInfo(nil), in.Calls...)
	}
	if len(in.RemotePartyNumbers) > 0 {
		out.RemotePartyNumbers = make([]qmi.VoiceRemotePartyNumber, len(in.RemotePartyNumbers))
		for i := range in.RemotePartyNumbers {
			out.RemotePartyNumbers[i] = in.RemotePartyNumbers[i]
			out.RemotePartyNumbers[i].RawNumber = cloneBytes(in.RemotePartyNumbers[i].RawNumber)
		}
	}
	return &out
}

func cloneVoiceUSSDPayload(in *qmi.VoiceUSSDPayload) *qmi.VoiceUSSDPayload {
	if in == nil {
		return nil
	}
	out := *in
	out.Data = cloneBytes(in.Data)
	return &out
}

func cloneVoiceUSSDIndication(in *qmi.VoiceUSSDIndication) *qmi.VoiceUSSDIndication {
	if in == nil {
		return nil
	}
	out := *in
	out.USSData = cloneVoiceUSSDPayload(in.USSData)
	out.USSDataUTF16 = cloneUint16s(in.USSDataUTF16)
	return &out
}

func cloneVoiceSupplementaryServiceIndication(in *qmi.VoiceSupplementaryServiceIndication) *qmi.VoiceSupplementaryServiceIndication {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}

func cloneVoiceSupplementaryServiceRequestIndication(in *qmi.VoiceSupplementaryServiceRequestIndication) *qmi.VoiceSupplementaryServiceRequestIndication {
	if in == nil {
		return nil
	}
	out := *in
	out.USSData = cloneVoiceUSSDPayload(in.USSData)
	out.Alpha = cloneVoiceUSSDPayload(in.Alpha)
	out.EncodedDataUTF16 = cloneUint16s(in.EncodedDataUTF16)
	return &out
}

func cloneVoiceUSSDNoWaitIndication(in *qmi.VoiceUSSDNoWaitIndication) *qmi.VoiceUSSDNoWaitIndication {
	if in == nil {
		return nil
	}
	out := *in
	out.USSData = cloneVoiceUSSDPayload(in.USSData)
	out.Alpha = cloneVoiceUSSDPayload(in.Alpha)
	out.USSDataUTF16 = cloneUint16s(in.USSDataUTF16)
	return &out
}

func cloneServingSystemForEvent(in *qmi.ServingSystem) *qmi.ServingSystem {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}

func cloneNASOperatorNameInfo(in *qmi.NASOperatorNameInfo) *qmi.NASOperatorNameInfo {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}

func cloneNetworkTimeInfo(in *qmi.NetworkTimeInfo) *qmi.NetworkTimeInfo {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}

func cloneSignalInfo(in *qmi.SignalInfo) *qmi.SignalInfo {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}

func cloneNASNetworkRejectInfo(in *qmi.NASNetworkRejectInfo) *qmi.NASNetworkRejectInfo {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}

func cloneScanResultsForEvent(in []qmi.NetworkScanResult) []qmi.NetworkScanResult {
	if len(in) == 0 {
		return nil
	}
	out := make([]qmi.NetworkScanResult, len(in))
	for i := range in {
		out[i] = in[i]
		out[i].RATs = cloneBytes(in[i].RATs)
	}
	return out
}

func cloneNASIncrementalNetworkScanInfo(in *qmi.NASIncrementalNetworkScanInfo) *qmi.NASIncrementalNetworkScanInfo {
	if in == nil {
		return nil
	}
	out := *in
	out.Results = cloneScanResultsForEvent(in.Results)
	return &out
}

func cloneUIMRefreshFiles(in []qmi.UIMRefreshFile) []qmi.UIMRefreshFile {
	if len(in) == 0 {
		return nil
	}
	out := make([]qmi.UIMRefreshFile, len(in))
	for i := range in {
		out[i] = in[i]
		out[i].Path = cloneBytes(in[i].Path)
	}
	return out
}

func cloneUIMRefreshIndication(in *qmi.UIMRefreshIndication) *qmi.UIMRefreshIndication {
	if in == nil {
		return nil
	}
	out := *in
	out.ApplicationIdentifier = cloneBytes(in.ApplicationIdentifier)
	out.Files = cloneUIMRefreshFiles(in.Files)
	return &out
}

func cloneUIMSlotStatus(in *qmi.UIMSlotStatus) *qmi.UIMSlotStatus {
	if in == nil {
		return nil
	}
	out := *in
	if len(in.Slots) > 0 {
		out.Slots = make([]qmi.UIMSlotStatusSlot, len(in.Slots))
		for i := range in.Slots {
			out.Slots[i] = in.Slots[i]
			out.Slots[i].ICCIDRaw = cloneBytes(in.Slots[i].ICCIDRaw)
			out.Slots[i].ATR = cloneBytes(in.Slots[i].ATR)
			out.Slots[i].EID = cloneBytes(in.Slots[i].EID)
		}
	}
	return &out
}

func cloneWMSSMSCAddress(in *qmi.WMSSMSCAddress) *qmi.WMSSMSCAddress {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}

func cloneTLVMeta(in []qmi.TLVMeta) []qmi.TLVMeta {
	if len(in) == 0 {
		return nil
	}
	out := make([]qmi.TLVMeta, len(in))
	copy(out, in)
	return out
}

func cloneEvent(event Event) Event {
	out := event
	out.Settings = cloneRuntimeSettings(event.Settings)
	out.Signal = cloneSignalStrengthForEvent(event.Signal)
	out.Pdu = cloneBytes(event.Pdu)
	out.IMSRegistration = cloneIMSARegistrationStatus(event.IMSRegistration)
	out.IMSServices = cloneIMSAServicesStatus(event.IMSServices)
	out.IMSSettings = cloneIMSServicesEnabledSettings(event.IMSSettings)
	out.VoiceCalls = cloneVoiceAllCallInfo(event.VoiceCalls)
	out.VoiceUSSD = cloneVoiceUSSDIndication(event.VoiceUSSD)
	out.VoiceSupplementary = cloneVoiceSupplementaryServiceIndication(event.VoiceSupplementary)
	out.VoiceSupplementaryRequest = cloneVoiceSupplementaryServiceRequestIndication(event.VoiceSupplementaryRequest)
	out.VoiceUSSDNoWait = cloneVoiceUSSDNoWaitIndication(event.VoiceUSSDNoWait)
	out.ServingSystem = cloneServingSystemForEvent(event.ServingSystem)
	out.NASOperatorName = cloneNASOperatorNameInfo(event.NASOperatorName)
	out.NASNetworkTime = cloneNetworkTimeInfo(event.NASNetworkTime)
	out.NASSignalInfo = cloneSignalInfo(event.NASSignalInfo)
	out.NASNetworkReject = cloneNASNetworkRejectInfo(event.NASNetworkReject)
	out.NASIncrementalNetwork = cloneNASIncrementalNetworkScanInfo(event.NASIncrementalNetwork)
	out.UIMRefresh = cloneUIMRefreshIndication(event.UIMRefresh)
	out.UIMSlotStatus = cloneUIMSlotStatus(event.UIMSlotStatus)
	out.WMSSMSCAddress = cloneWMSSMSCAddress(event.WMSSMSCAddress)
	out.TLVMeta = cloneTLVMeta(event.TLVMeta)
	return out
}

func (e *EventEmitter) loop() {
	for {
		select {
		case <-e.done:
			return
		case event := <-e.queue:
			e.mu.RLock()
			handlers := make([]EventHandler, len(e.handlers))
			copy(handlers, e.handlers)
			e.mu.RUnlock()

			for _, handler := range handlers {
				func(h EventHandler) {
					defer func() {
						if recover() != nil {
							// Keep the emitter alive even if a callback panics.
						}
					}()
					h(cloneEvent(event))
				}(handler)
			}
		}
	}
}

// On registers an event handler / On 注册事件处理器
func (e *EventEmitter) On(handler EventHandler) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.handlers = append(e.handlers, handler)
}

// Emit sends an event to all handlers / Emit 向所有处理器发送事件
func (e *EventEmitter) Emit(event Event) bool {
	if e == nil {
		return false
	}
	select {
	case <-e.done:
		e.dropped.Add(1)
		return false
	default:
	}
	select {
	case <-e.done:
		e.dropped.Add(1)
		return false
	case e.queue <- event:
		return true
	default:
		e.dropped.Add(1)
		return false
	}
}

func (e *EventEmitter) Close() {
	if e == nil || e.done == nil {
		return
	}
	e.closeOnce.Do(func() {
		close(e.done)
	})
}

func (e *EventEmitter) Dropped() uint64 {
	if e == nil {
		return 0
	}
	return e.dropped.Load()
}

// Clear removes all handlers / Clear 移除所有处理器
func (e *EventEmitter) Clear() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.handlers = e.handlers[:0]
}

// ============================================================================
// Convenience Methods on Manager / Manager 便捷方法
// ============================================================================

// OnEvent registers a callback for all events / OnEvent 为所有事件注册回调
func (m *Manager) OnEvent(handler EventHandler) {
	if m.events == nil {
		m.events = NewEventEmitter()
	}
	m.events.On(handler)
}

// OnConnect registers a callback for connect events / OnConnect 为连接事件注册回调
func (m *Manager) OnConnect(handler func(settings *qmi.RuntimeSettings)) {
	m.OnEvent(func(e Event) {
		if e.Type == EventConnected && handler != nil {
			handler(e.Settings)
		}
	})
}

// OnDisconnect registers a callback for disconnect events / OnDisconnect 为断开连接事件注册回调
func (m *Manager) OnDisconnect(handler func()) {
	m.OnEvent(func(e Event) {
		if e.Type == EventDisconnected && handler != nil {
			handler()
		}
	})
}

// OnIPChange registers a callback for IP change events / OnIPChange 为 IP 变化事件注册回调
func (m *Manager) OnIPChange(handler func(settings *qmi.RuntimeSettings)) {
	m.OnEvent(func(e Event) {
		if e.Type == EventIPChanged && handler != nil {
			handler(e.Settings)
		}
	})
}

// OnError registers a callback for error events / OnError 为错误事件注册回调
func (m *Manager) OnError(handler func(err error)) {
	m.OnEvent(func(e Event) {
		if e.Type == EventDialFailed && handler != nil {
			handler(e.Error)
		}
	})
}

// OnIMSRegistrationStatus registers a callback for IMSA registration status indications.
func (m *Manager) OnIMSRegistrationStatus(handler func(info *qmi.IMSARegistrationStatus)) {
	m.OnEvent(func(e Event) {
		if e.Type == EventIMSRegistrationStatus && handler != nil {
			handler(e.IMSRegistration)
		}
	})
}

// OnIMSServicesStatus registers a callback for IMSA services status indications.
func (m *Manager) OnIMSServicesStatus(handler func(info *qmi.IMSAServicesStatus)) {
	m.OnEvent(func(e Event) {
		if e.Type == EventIMSServicesStatus && handler != nil {
			handler(e.IMSServices)
		}
	})
}

// OnIMSSettingsChanged registers a callback for IMS settings change indications.
func (m *Manager) OnIMSSettingsChanged(handler func(info *qmi.IMSServicesEnabledSettings)) {
	m.OnEvent(func(e Event) {
		if e.Type == EventIMSSettingsChanged && handler != nil {
			handler(e.IMSSettings)
		}
	})
}

// OnVoiceCallStatus registers a callback for voice call status indications.
func (m *Manager) OnVoiceCallStatus(handler func(info *qmi.VoiceAllCallInfo)) {
	m.OnEvent(func(e Event) {
		if e.Type == EventVoiceCallStatus && handler != nil {
			handler(e.VoiceCalls)
		}
	})
}

// OnVoiceUSSD registers a callback for voice USSD indications.
func (m *Manager) OnVoiceUSSD(handler func(info *qmi.VoiceUSSDIndication)) {
	m.OnEvent(func(e Event) {
		if e.Type == EventVoiceUSSD && handler != nil {
			handler(e.VoiceUSSD)
		}
	})
}

// OnVoiceUSSDReleased registers a callback for USSD release indications.
func (m *Manager) OnVoiceUSSDReleased(handler func()) {
	m.OnEvent(func(e Event) {
		if e.Type == EventVoiceUSSDReleased && handler != nil {
			handler()
		}
	})
}

// OnVoiceSupplementaryService registers a callback for supplementary service indications.
func (m *Manager) OnVoiceSupplementaryService(handler func(info *qmi.VoiceSupplementaryServiceIndication)) {
	m.OnEvent(func(e Event) {
		if e.Type == EventVoiceSupplementaryService && handler != nil {
			handler(e.VoiceSupplementary)
		}
	})
}

// OnVoiceSupplementaryServiceRequest registers a callback for supplementary service request indications.
func (m *Manager) OnVoiceSupplementaryServiceRequest(handler func(info *qmi.VoiceSupplementaryServiceRequestIndication)) {
	m.OnEvent(func(e Event) {
		if e.Type == EventVoiceSupplementaryServiceRequest && handler != nil {
			handler(e.VoiceSupplementaryRequest)
		}
	})
}

// OnVoiceUSSDNoWaitResult registers a callback for async USSD results.
func (m *Manager) OnVoiceUSSDNoWaitResult(handler func(info *qmi.VoiceUSSDNoWaitIndication)) {
	m.OnEvent(func(e Event) {
		if e.Type == EventVoiceUSSDNoWaitResult && handler != nil {
			handler(e.VoiceUSSDNoWait)
		}
	})
}

// OnNASServingSystemChanged registers a callback for NAS serving-system indications.
func (m *Manager) OnNASServingSystemChanged(handler func(info *qmi.ServingSystem)) {
	m.OnEvent(func(e Event) {
		if e.Type == EventServingSystemChanged && handler != nil {
			handler(e.ServingSystem)
		}
	})
}

// OnNASOperatorNameChanged registers a callback for NAS operator-name indications.
func (m *Manager) OnNASOperatorNameChanged(handler func(info *qmi.NASOperatorNameInfo)) {
	m.OnEvent(func(e Event) {
		if e.Type == EventNASOperatorNameChanged && handler != nil {
			handler(e.NASOperatorName)
		}
	})
}

// OnNASNetworkTimeChanged registers a callback for NAS network-time indications.
func (m *Manager) OnNASNetworkTimeChanged(handler func(info *qmi.NetworkTimeInfo)) {
	m.OnEvent(func(e Event) {
		if e.Type == EventNASNetworkTimeChanged && handler != nil {
			handler(e.NASNetworkTime)
		}
	})
}

// OnNASSignalInfoChanged registers a callback for NAS signal-info indications.
func (m *Manager) OnNASSignalInfoChanged(handler func(info *qmi.SignalInfo)) {
	m.OnEvent(func(e Event) {
		if e.Type == EventNASSignalInfoChanged && handler != nil {
			handler(e.NASSignalInfo)
		}
	})
}

// OnNASNetworkReject registers a callback for NAS network-reject indications.
func (m *Manager) OnNASNetworkReject(handler func(info *qmi.NASNetworkRejectInfo)) {
	m.OnEvent(func(e Event) {
		if e.Type == EventNASNetworkReject && handler != nil {
			handler(e.NASNetworkReject)
		}
	})
}

// OnNASIncrementalNetworkScan registers a callback for NAS incremental-scan indications.
func (m *Manager) OnNASIncrementalNetworkScan(handler func(info *qmi.NASIncrementalNetworkScanInfo)) {
	m.OnEvent(func(e Event) {
		if e.Type == EventNASIncrementalNetworkScan && handler != nil {
			handler(e.NASIncrementalNetwork)
		}
	})
}

// OnUIMRefresh registers a callback for UIM refresh indications.
func (m *Manager) OnUIMRefresh(handler func(info *qmi.UIMRefreshIndication)) {
	m.OnEvent(func(e Event) {
		if e.Type == EventUIMRefresh && handler != nil {
			handler(e.UIMRefresh)
		}
	})
}

// OnUIMSlotStatus registers a callback for UIM slot status indications.
func (m *Manager) OnUIMSlotStatus(handler func(info *qmi.UIMSlotStatus)) {
	m.OnEvent(func(e Event) {
		if e.Type == EventUIMSlotStatus && handler != nil {
			handler(e.UIMSlotStatus)
		}
	})
}
