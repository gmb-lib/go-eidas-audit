package eidas

import (
	"azugo.io/core/validation"
	"github.com/spf13/viper"
)

// Configuration is the (small) eIDAS-audit emitter configuration, bound as a
// sub-configuration of a consuming service. Only the broker topic is
// configurable; the broker connection itself is owned by go-platform-kit's
// config.Broker. Most services can rely on the default and skip this.
type Configuration struct {
	// Topic is the broker topic eIDAS-audit signing-evidence events are published
	// to (env EIDAS_AUDIT_TOPIC). Defaults to DefaultTopic.
	Topic string `mapstructure:"topic" validate:"required"`
}

// Bind registers defaults and the environment binding under prefix.
func (c *Configuration) Bind(prefix string, v *viper.Viper) {
	v.SetDefault(prefix+".topic", DefaultTopic)

	_ = v.BindEnv(prefix+".topic", "EIDAS_AUDIT_TOPIC")
}

// Validate validates the configuration.
func (c *Configuration) Validate(valid *validation.Validate) error {
	return valid.Struct(c)
}
