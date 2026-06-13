package config

import (
	mapperx "github.com/arcgolabs/mapper"
	"github.com/samber/oops"
)

type upstreamConfigMapper struct {
	mapper *mapperx.Mapper
}

func newUpstreamConfigMapper() *upstreamConfigMapper {
	return &upstreamConfigMapper{
		mapper: mapperx.New(),
	}
}

func (m *upstreamConfigMapper) ContainerRegistryToUpstream(cfg ContainerRegistryConfig) (UpstreamConfig, error) {
	upstreamCfg, err := mapUpstreamConfig[UpstreamConfig](m, cfg)
	if err != nil {
		return UpstreamConfig{}, err
	}
	upstreamCfg.Type = ecosystemContainer
	return upstreamCfg, nil
}

func (m *upstreamConfigMapper) UpstreamToContainerRegistry(upstreamCfg UpstreamConfig) (ContainerRegistryConfig, error) {
	return mapUpstreamConfig[ContainerRegistryConfig](m, upstreamCfg)
}

func (m *upstreamConfigMapper) DependencyUpstreamToUpstream(cfg DependencyUpstreamConfig, ecosystem string) (UpstreamConfig, error) {
	upstreamCfg, err := mapUpstreamConfig[UpstreamConfig](m, cfg)
	if err != nil {
		return UpstreamConfig{}, err
	}
	upstreamCfg.Type = ecosystem
	return upstreamCfg, nil
}

func (m *upstreamConfigMapper) UpstreamToDependencyUpstream(upstreamCfg UpstreamConfig) (DependencyUpstreamConfig, error) {
	return mapUpstreamConfig[DependencyUpstreamConfig](m, upstreamCfg)
}

func (m *upstreamConfigMapper) DistUpstreamToUpstream(cfg DistUpstreamConfig) (UpstreamConfig, error) {
	upstreamCfg, err := mapUpstreamConfig[UpstreamConfig](m, cfg)
	if err != nil {
		return UpstreamConfig{}, err
	}
	upstreamCfg.Type = ecosystemDist
	return upstreamCfg, nil
}

func (m *upstreamConfigMapper) UpstreamToDistUpstream(upstreamCfg UpstreamConfig) (DistUpstreamConfig, error) {
	return mapUpstreamConfig[DistUpstreamConfig](m, upstreamCfg)
}

func mapUpstreamConfig[D any](m *upstreamConfigMapper, src any) (D, error) {
	var dst D
	if m == nil || m.mapper == nil {
		m = newUpstreamConfigMapper()
	}
	if err := m.mapper.MapInto(&dst, src); err != nil {
		return dst, oops.In("config").Wrapf(err, "map upstream config")
	}
	return dst, nil
}

func mustMapUpstreamConfig[D any](value D, err error) D {
	if err != nil {
		panic(err)
	}
	return value
}
