package manager

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"
)

// ============================================================================
// ModemPool - 多模组管理池
// 管理多个 4G 模组，提供负载均衡和批量操作
// ============================================================================

// ModemPool manages multiple modem connections / ModemPool 管理多个模组连接
type ModemPool struct {
	mu       sync.RWMutex
	modems   map[string]*Manager // key = interface name (e.g. "wwan0") / key = 接口名
	selector Selector            // Load balancing strategy / 负载均衡策略
	log      Logger
	events   *EventEmitter

	// Health monitoring / 健康监控
	healthCtx    context.Context
	healthCancel context.CancelFunc
	healthStatus map[string]*HealthStatus

	// Hot-plug / 热插拔
	hotplugCtx    context.Context
	hotplugCancel context.CancelFunc
	baseCfg       Config // Base config for auto-discovered modems / 自动发现模组的基础配置
	baseLogger    Logger // Logger for auto-discovered modems / 自动发现模组的日志器
}

// HealthStatus represents the health of a modem / HealthStatus 表示模组的健康状态
type HealthStatus struct {
	Name       string    // Modem name / 模组名称
	Connected  bool      // Is connected / 是否已连接
	SignalRSSI int8      // Signal strength (dBm) / 信号强度 (dBm)
	LastCheck  time.Time // Last health check time / 上次健康检查时间
	LastError  error     // Last error if any / 上次错误 (如有)
}

// NewPool creates a new modem pool / NewPool 创建新的模组池
func NewPool() *ModemPool {
	return &ModemPool{
		modems:       make(map[string]*Manager),
		selector:     &RoundRobinSelector{},
		log:          NewNopLogger(),
		events:       NewEventEmitter(),
		healthStatus: make(map[string]*HealthStatus),
	}
}

// SetLogger sets the logger for the pool / SetLogger 设置池的日志器
func (p *ModemPool) SetLogger(logger Logger) {
	p.log = logger.WithField("component", "pool")
}

// SetSelector sets the modem selection strategy / SetSelector 设置模组选择策略
func (p *ModemPool) SetSelector(s Selector) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.selector = s
}

// ============================================================================
// 模组管理
// ============================================================================

// Add adds a managed modem to the pool / Add 向池中添加托管的模组
func (p *ModemPool) Add(name string, cfg Config, logger Logger) (*Manager, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if _, exists := p.modems[name]; exists {
		return nil, fmt.Errorf("modem %s already exists in pool", name)
	}

	mgr := New(cfg, logger)
	p.modems[name] = mgr
	p.log.Infof("Added modem: %s (%s)", name, cfg.Device.NetInterface)
	return mgr, nil
}

// Remove removes a modem from the pool / Remove 从池中移除模组
func (p *ModemPool) Remove(name string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	mgr, exists := p.modems[name]
	if !exists {
		return fmt.Errorf("modem %s not found in pool", name)
	}

	mgr.Stop()
	delete(p.modems, name)
	p.log.Infof("Removed modem: %s", name)
	return nil
}

// Get returns a modem using the configured selector / Get 使用配置的选择器返回模组
func (p *ModemPool) Get() *Manager {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if len(p.modems) == 0 {
		return nil
	}

	managers := p.getConnectedManagers()
	if len(managers) == 0 {
		// No connected modems, return any / 没有已连接的模组，返回任意一个
		for _, mgr := range p.modems {
			return mgr
		}
	}

	return p.selector.Select(managers)
}

// GetByName returns a specific modem by name / GetByName 通过名称获取特定模组
func (p *ModemPool) GetByName(name string) *Manager {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.modems[name]
}

// GetHealthy returns a connected modem with best signal / GetHealthy 返回信号最好的已连接模组
func (p *ModemPool) GetHealthy() *Manager {
	p.mu.RLock()
	defer p.mu.RUnlock()

	managers := p.getConnectedManagers()
	if len(managers) == 0 {
		return nil
	}

	// Use signal strength selector for healthy check / 使用信号强度选择器进行健康检查
	selector := &SignalStrengthSelector{}
	return selector.Select(managers)
}

// All returns a copy of all modems / All 返回所有模组的副本
func (p *ModemPool) All() map[string]*Manager {
	p.mu.RLock()
	defer p.mu.RUnlock()

	result := make(map[string]*Manager, len(p.modems))
	for k, v := range p.modems {
		result[k] = v
	}
	return result
}

// Size returns the number of modems in pool / Size 返回池中模组数量
func (p *ModemPool) Size() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.modems)
}

// ConnectedCount returns the number of connected modems / ConnectedCount 返回已连接的模组数量
func (p *ModemPool) ConnectedCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.getConnectedManagers())
}

func (p *ModemPool) getConnectedManagers() []*Manager {
	var result []*Manager
	for _, mgr := range p.modems {
		if mgr.State() == StateConnected {
			result = append(result, mgr)
		}
	}
	return result
}

// ============================================================================
// 批量操作
// ============================================================================

// StartAll starts all modems / StartAll 启动所有模组
func (p *ModemPool) StartAll() error {
	p.mu.RLock()
	modems := make([]*Manager, 0, len(p.modems))
	for _, mgr := range p.modems {
		modems = append(modems, mgr)
	}
	p.mu.RUnlock()

	var wg sync.WaitGroup
	errCh := make(chan error, len(modems))

	for _, mgr := range modems {
		wg.Add(1)
		go func(m *Manager) {
			defer wg.Done()
			if err := m.Start(); err != nil {
				errCh <- err
			}
		}(mgr)
	}

	wg.Wait()
	close(errCh)

	// Collect errors / 收集错误
	var errs []error
	for err := range errCh {
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		return fmt.Errorf("failed to start %d modems", len(errs))
	}
	return nil
}

// StopAll stops all modems / StopAll 停止所有模组
func (p *ModemPool) StopAll() {
	p.mu.RLock()
	modems := make([]*Manager, 0, len(p.modems))
	for _, mgr := range p.modems {
		modems = append(modems, mgr)
	}
	p.mu.RUnlock()

	var wg sync.WaitGroup
	for _, mgr := range modems {
		wg.Add(1)
		go func(m *Manager) {
			defer wg.Done()
			m.Stop()
		}(mgr)
	}
	wg.Wait()
}

// ============================================================================
// 自动发现
// ============================================================================

// DiscoverAndAdd discovers modems and adds them to pool / DiscoverAndAdd 发现模组并添加到池
func (p *ModemPool) DiscoverAndAdd(baseCfg Config, logger Logger) (int, error) {
	if discoverModemsFn == nil {
		return 0, fmt.Errorf("no device discoverer registered; import github.com/iniwex5/quectel-qmi-go/pkg/device or inject devices manually")
	}
	modems, err := discoverModemsFn()
	if err != nil {
		return 0, err
	}

	added := 0
	for _, modem := range modems {
		cfg := baseCfg
		cfg.Device = modem

		_, err := p.Add(modem.NetInterface, cfg, logger)
		if err != nil {
			p.log.WithError(err).Warnf("Failed to add modem %s", modem.NetInterface)
			continue
		}
		added++
	}

	return added, nil
}

// ============================================================================
// 健康监控
// ============================================================================

// StartHealthMonitor starts periodic health checks / StartHealthMonitor 启动定期健康检查
func (p *ModemPool) StartHealthMonitor(interval time.Duration) {
	if p.healthCtx != nil {
		return // Already running / 已在运行
	}

	p.healthCtx, p.healthCancel = context.WithCancel(context.Background())
	go p.healthLoop(interval)
	p.log.Infof("Health monitor started (interval=%s)", interval)
}

// StopHealthMonitor stops the health monitor / StopHealthMonitor 停止健康监控
func (p *ModemPool) StopHealthMonitor() {
	if p.healthCancel != nil {
		p.healthCancel()
		p.healthCancel = nil
		p.healthCtx = nil
		p.log.Info("Health monitor stopped")
	}
}

func (p *ModemPool) healthLoop(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-p.healthCtx.Done():
			return
		case <-ticker.C:
			p.checkHealth()
		}
	}
}

func (p *ModemPool) checkHealth() {
	p.mu.RLock()
	names := make([]string, 0, len(p.modems))
	modems := make([]*Manager, 0, len(p.modems))
	for name, mgr := range p.modems {
		names = append(names, name)
		modems = append(modems, mgr)
	}
	p.mu.RUnlock()

	for i, mgr := range modems {
		name := names[i]
		status := &HealthStatus{
			Name:      name,
			LastCheck: time.Now(),
		}

		status.Connected = mgr.State() == StateConnected

		if mgr.nas != nil {
			sig, err := mgr.nas.GetSignalStrength(context.Background())
			if err != nil {
				status.LastError = err
			} else {
				status.SignalRSSI = sig.RSSI
			}
		}

		p.mu.Lock()
		p.healthStatus[name] = status
		p.mu.Unlock()
	}
}

// Health returns the current health status of all modems / Health 返回所有模组的当前健康状态
func (p *ModemPool) Health() map[string]*HealthStatus {
	p.mu.RLock()
	defer p.mu.RUnlock()

	result := make(map[string]*HealthStatus, len(p.healthStatus))
	for k, v := range p.healthStatus {
		result[k] = v
	}
	return result
}

// ============================================================================
// 热插拔监控
// ============================================================================

// PoolEventType represents pool-level events / PoolEventType 表示池级别事件
type PoolEventType int

const (
	PoolEventModemAdded   PoolEventType = iota // Modem added to pool / 模组添加到池
	PoolEventModemRemoved                      // Modem removed from pool / 模组从池移除
)

// PoolEvent represents a pool-level event / PoolEvent 表示池级别事件
type PoolEvent struct {
	Type  PoolEventType
	Name  string // Modem name / 模组名称
	Modem *Manager
}

// PoolEventHandler is a callback for pool events / PoolEventHandler 是池事件的回调
type PoolEventHandler func(event PoolEvent)

// WatchHotPlug starts watching for modem hot-plug events / WatchHotPlug 开始监控模组热插拔事件
func (p *ModemPool) WatchHotPlug(baseCfg Config, logger Logger, interval time.Duration) {
	if p.hotplugCtx != nil {
		return // Already running / 已在运行
	}

	p.baseCfg = baseCfg
	p.baseLogger = logger
	p.hotplugCtx, p.hotplugCancel = context.WithCancel(context.Background())
	go p.hotplugLoop(interval)
	p.log.Infof("Hot-plug watcher started (interval=%s)", interval)
}

// StopHotPlug stops watching for hot-plug events / StopHotPlug 停止监控热插拔
func (p *ModemPool) StopHotPlug() {
	if p.hotplugCancel != nil {
		p.hotplugCancel()
		p.hotplugCancel = nil
		p.hotplugCtx = nil
		p.log.Info("Hot-plug watcher stopped")
	}
}

func (p *ModemPool) hotplugLoop(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-p.hotplugCtx.Done():
			return
		case <-ticker.C:
			p.scanForChanges()
		}
	}
}

func (p *ModemPool) scanForChanges() {
	if discoverModemsFn == nil {
		p.log.Debug("Hot-plug scan skipped: no device discoverer registered")
		return
	}
	// Discover currently available modems / 发现当前可用的模组
	discovered, err := discoverModemsFn()
	if err != nil {
		p.log.WithError(err).Debug("Hot-plug scan failed")
		return
	}

	discoveredMap := make(map[string]ModemDevice)
	for _, m := range discovered {
		discoveredMap[m.NetInterface] = m
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	// Check for removed modems / 检查移除的模组
	for name, mgr := range p.modems {
		if _, exists := discoveredMap[name]; !exists {
			p.log.Warnf("Hot-plug: modem %s removed", name)
			mgr.Stop()
			delete(p.modems, name)
			delete(p.healthStatus, name)
			// Emit event / 发送事件
			p.events.Emit(Event{Type: EventDisconnected})
		}
	}

	// Check for new modems / 检查新增的模组
	for name, dev := range discoveredMap {
		if _, exists := p.modems[name]; !exists {
			p.log.Infof("Hot-plug: new modem %s detected", name)
			cfg := p.baseCfg
			cfg.Device = dev
			mgr := New(cfg, p.baseLogger)
			p.modems[name] = mgr
			// Auto-start the new modem / 自动启动新模组
			go func(m *Manager) {
				if err := m.Start(); err != nil {
					p.log.WithError(err).Warnf("Failed to start hot-plugged modem")
				}
			}(mgr)
		}
	}
}

// ============================================================================
// 事件
// ============================================================================

// OnEvent registers a callback for pool events / OnEvent 为池事件注册回调
func (p *ModemPool) OnEvent(handler EventHandler) {
	p.events.On(handler)
}

// ============================================================================
// Selector Interface - 选择器接口
// ============================================================================

// Selector defines the interface for modem selection strategies
// Selector 定义模组选择策略的接口
type Selector interface {
	Select(modems []*Manager) *Manager
}

// ============================================================================
// RoundRobinSelector - 轮询选择器
// ============================================================================

// RoundRobinSelector selects modems in round-robin order / RoundRobinSelector 按轮询顺序选择模组
type RoundRobinSelector struct {
	counter uint64
}

func (s *RoundRobinSelector) Select(modems []*Manager) *Manager {
	if len(modems) == 0 {
		return nil
	}
	idx := atomic.AddUint64(&s.counter, 1) % uint64(len(modems))
	return modems[idx]
}

// ============================================================================
// RandomSelector - 随机选择器
// ============================================================================

// RandomSelector selects a random modem / RandomSelector 随机选择模组
type RandomSelector struct{}

func (s *RandomSelector) Select(modems []*Manager) *Manager {
	if len(modems) == 0 {
		return nil
	}
	return modems[rand.Intn(len(modems))]
}

// ============================================================================
// SignalStrengthSelector - 信号强度选择器
// ============================================================================

// SignalStrengthSelector selects the modem with best signal / SignalStrengthSelector 选择信号最强的模组
type SignalStrengthSelector struct{}

func (s *SignalStrengthSelector) Select(modems []*Manager) *Manager {
	if len(modems) == 0 {
		return nil
	}

	var best *Manager
	bestRSSI := int8(-128) // Minimum possible value / 最小可能值

	for _, mgr := range modems {
		if mgr.nas != nil {
			sig, err := mgr.nas.GetSignalStrength(context.Background())
			if err == nil && sig.RSSI > bestRSSI {
				bestRSSI = sig.RSSI
				best = mgr
			}
		}
	}

	if best == nil {
		// Fallback to first modem if signal check failed / 如果信号检查失败，回退到第一个模组
		return modems[0]
	}
	return best
}

// ============================================================================
// LeastUsedSelector - 最少使用选择器
// ============================================================================

// LeastUsedSelector selects the least recently used modem / LeastUsedSelector 选择最近最少使用的模组
type LeastUsedSelector struct {
	mu       sync.Mutex
	lastUsed map[*Manager]time.Time
}

func NewLeastUsedSelector() *LeastUsedSelector {
	return &LeastUsedSelector{
		lastUsed: make(map[*Manager]time.Time),
	}
}

func (s *LeastUsedSelector) Select(modems []*Manager) *Manager {
	if len(modems) == 0 {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	var leastUsed *Manager
	var leastTime time.Time

	for i, mgr := range modems {
		t, exists := s.lastUsed[mgr]
		if !exists {
			// Never used, select this one / 从未使用过，选择这个
			s.lastUsed[mgr] = time.Now()
			return mgr
		}
		if i == 0 || t.Before(leastTime) {
			leastTime = t
			leastUsed = mgr
		}
	}

	s.lastUsed[leastUsed] = time.Now()
	return leastUsed
}
