package main

import (
	"github.com/brave-experiments/nitro-enclave-kubelet/cmd/internal/provider"
	"github.com/brave-experiments/nitro-enclave-kubelet/cmd/internal/provider/enclave"
	"github.com/brave-experiments/nitro-enclave-kubelet/cmd/internal/provider/mock"
)

func registerMock(s *provider.Store) {
	/* #nosec */
	s.Register("mock", func(cfg provider.InitConfig) (provider.Provider, error) { //nolint:errcheck
		return mock.NewMockProvider(
			cfg.ConfigPath,
			cfg.NodeName,
			cfg.OperatingSystem,
			cfg.InternalIP,
			cfg.DaemonPort,
		)
	})
}

func registerEnclave(s *provider.Store) {
	/* #nosec */
	s.Register("enclave", func(cfg provider.InitConfig) (provider.Provider, error) { //nolint:errcheck
		return enclave.NewEnclaveProvider(
			cfg.ConfigPath,
			cfg.NodeName,
			cfg.OperatingSystem,
			cfg.InternalIP,
			cfg.DaemonPort,
		)
	})
}
