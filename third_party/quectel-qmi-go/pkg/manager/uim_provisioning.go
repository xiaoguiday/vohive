package manager

import (
	"context"
	"errors"
	"time"

	"github.com/iniwex5/quectel-qmi-go/pkg/qmi"
)

// EnsureSIMProvisionedOptions 控制 provisioning 收敛的轮询节奏。
type EnsureSIMProvisionedOptions struct {
	DefaultSlot             uint8
	PollInterval            time.Duration
	MaxAttempts             int
	UnknownAppStateBackstop int
	MaxActivations          int           // 整个调用内最多激活次数（含 deactivate→activate）
	ActivationSettle        time.Duration // 一次激活后留给卡收敛的观察等待
}

func normalizeEnsureSIMProvisionedOptions(o EnsureSIMProvisionedOptions) EnsureSIMProvisionedOptions {
	if o.DefaultSlot == 0 {
		o.DefaultSlot = 1
	}
	if o.PollInterval <= 0 {
		o.PollInterval = 500 * time.Millisecond
	}
	if o.MaxAttempts <= 0 {
		o.MaxAttempts = 10
	}
	if o.UnknownAppStateBackstop <= 0 {
		o.UnknownAppStateBackstop = 3
	}
	if o.MaxActivations <= 0 {
		o.MaxActivations = 2
	}
	if o.ActivationSettle <= 0 {
		o.ActivationSettle = 2 * time.Second
	}
	return o
}

type ensureProvisioningDeps struct {
	readiness func(context.Context) (UIMReadiness, error)
	usimAID   func(context.Context) ([]byte, error)
	rebind    func(ctx context.Context, slot uint8, aid []byte) error
	sleep     func(ctx context.Context, d time.Duration) error
}

// EnsureSIMProvisioned 幂等地把 USIM 从 detected 收敛到 ready：
// 仅当卡在场且应用 detected（或 AppState 未知且持续未就绪）时才激活
// primary-gw provisioning session；对已 ready 设备完全 no-op。
func (m *Manager) EnsureSIMProvisioned(ctx context.Context, opts EnsureSIMProvisionedOptions) (UIMReadiness, error) {
	return ensureSIMProvisioned(ctx, opts, ensureProvisioningDeps{
		readiness: m.GetUIMReadiness,
		usimAID:   m.GetUSIMAID,
		rebind:    m.UIMRebindPrimaryGWProvisioning,
		sleep:     sleepWithContext,
	})
}

func ensureSIMProvisioned(ctx context.Context, opts EnsureSIMProvisionedOptions, deps ensureProvisioningDeps) (UIMReadiness, error) {
	opts = normalizeEnsureSIMProvisionedOptions(opts)
	var last UIMReadiness
	unknownStreak := 0
	activations := 0

	for attempt := 1; attempt <= opts.MaxAttempts; attempt++ {
		r, err := deps.readiness(ctx)
		if err != nil {
			return r, err
		}
		last = r

		if r.UIMReady {
			return r, nil // 幂等：已可用，零打扰。
		}
		switch r.Reason {
		case UIMReadinessCardAbsent, UIMReadinessSIMBlocked,
			UIMReadinessTransportFatal, UIMReadinessControlUnavailable:
			return r, nil
		}

		wantActivate := false
		switch {
		case r.NeedsProvisioning:
			wantActivate = true
			unknownStreak = 0
		case r.AppState == qmi.UIMAppStateUnknown:
			unknownStreak++
			if unknownStreak >= opts.UnknownAppStateBackstop {
				wantActivate = true
			}
		}

		// 仅在仍有激活预算时激活；激活后进入更长的 settle 观察，期间不再重绑。
		if wantActivate && activations < opts.MaxActivations {
			aid, aidErr := deps.usimAID(ctx)
			if aidErr != nil || len(aid) == 0 {
				// 非致命：读不到 AID，降级为旧行为，继续轮询。
			} else {
				// 无论 rebind 成败均消耗预算，防止瞬时错误导致抖动重绑。
				activations++
				if rbErr := deps.rebind(ctx, resolveUIMReloadSlot(r, opts.DefaultSlot), aid); rbErr != nil {
					var nse *qmi.NotSupportedError
					if errors.As(rbErr, &nse) {
						return r, nil // 模组自管理 provisioning，停止尝试。
					}
					// 其他瞬时错误：预算已消耗，回落普通 poll 继续等待。
				} else {
					unknownStreak = 0
					if attempt < opts.MaxAttempts {
						if slErr := deps.sleep(ctx, opts.ActivationSettle); slErr != nil {
							return last, slErr
						}
					}
					continue // settle 后直接进入下一轮复查，不落普通 poll。
				}
			}
		}

		if attempt < opts.MaxAttempts {
			if slErr := deps.sleep(ctx, opts.PollInterval); slErr != nil {
				return last, slErr
			}
		}
	}
	return last, nil
}
