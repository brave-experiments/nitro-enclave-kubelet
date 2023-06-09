package main

import (
	"context"

	"github.com/brave-experiments/nitro-enclave-kubelet/cmd/internal/provider"
	"github.com/brave-experiments/nitro-enclave-kubelet/cmd/internal/provider/enclave"
)

func registerEnclave(ctx context.Context, s *provider.Store) {
	/* #nosec */
	s.Register("enclave", func(cfg provider.InitConfig) (provider.Provider, error) { //nolint:errcheck
		return enclave.NewEnclaveProvider(
			ctx,
			cfg.ConfigPath,
			cfg.NodeName,
			cfg.OperatingSystem,
			cfg.InternalIP,
			cfg.DaemonPort,
		)
	})
}
