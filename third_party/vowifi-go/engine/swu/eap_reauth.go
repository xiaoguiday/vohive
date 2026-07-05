package swu

import (
	"strings"

	"github.com/iniwex5/vowifi-go/engine/swu/eapaka"
)

type EAPReauthenticationState struct {
	Identity            string
	Counter             uint16
	CounterOK           bool
	Keys                eapaka.Keys
	NextPseudonym       string
	Reauthenticated     bool
	CounterTooSmall     bool
	LastAcceptedCounter uint16
	LastRejectedCounter uint16
}

func (s EAPReauthenticationState) Usable() bool {
	return strings.TrimSpace(s.Identity) != "" && len(s.Keys.KAut) > 0 && len(s.Keys.KEncr) > 0
}

func (s EAPReauthenticationState) clone() EAPReauthenticationState {
	s.Identity = strings.TrimSpace(s.Identity)
	s.NextPseudonym = strings.TrimSpace(s.NextPseudonym)
	s.Keys = cloneEAPAKAKeys(s.Keys)
	return s
}

func cloneEAPAKAKeys(keys eapaka.Keys) eapaka.Keys {
	return eapaka.Keys{
		MK:      append([]byte(nil), keys.MK...),
		KEncr:   append([]byte(nil), keys.KEncr...),
		KAut:    append([]byte(nil), keys.KAut...),
		KRe:     append([]byte(nil), keys.KRe...),
		MSK:     append([]byte(nil), keys.MSK...),
		EMSK:    append([]byte(nil), keys.EMSK...),
		CKPrime: append([]byte(nil), keys.CKPrime...),
		IKPrime: append([]byte(nil), keys.IKPrime...),
	}
}
