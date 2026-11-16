// Essera fork: hooks the Essera integration (package essera) installs at start-up. The model
// package cannot import it, so the calls go through these variables; unset, upstream
// behaviour is unchanged.
package model

// ResolveChannelKey turns a channel key that is a reference to a secret into the secret's
// value. handled is false for an ordinary key.
var ResolveChannelKey func(channelKey string) (value string, handled bool, err error)

func resolveChannelKey(channelKey string) (string, bool, error) {
	if ResolveChannelKey == nil {
		return "", false, nil
	}
	return ResolveChannelKey(channelKey)
}
