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
	upstreamCfg, err := m.mapUpstreamConfig[UpstreamConfig](cfg)
	if err != nil {
		return UpstreamConfig{}, err
	}
	upstreamCfg.Type = ecosystemContainer
	return upstreamCfg, nil
}

func (m *upstreamConfigMapper) UpstreamToContainerRegistry(upstreamCfg UpstreamConfig) (ContainerRegistryConfig, error) {
	return m.mapUpstreamConfig[ContainerRegistryConfig](upstreamCfg)
}

func (m *upstreamConfigMapper) DependencyUpstreamToUpstream(cfg DependencyUpstreamConfig, ecosystem string) (UpstreamConfig, error) {
	upstreamCfg, err := m.mapUpstreamConfig[UpstreamConfig](cfg)
	if err != nil {
		return UpstreamConfig{}, err
	}
	upstreamCfg.Type = ecosystem
	return upstreamCfg, nil
}

func (m *upstreamConfigMapper) UpstreamToDependencyUpstream(upstreamCfg UpstreamConfig) (DependencyUpstreamConfig, error) {
	return m.mapUpstreamConfig[DependencyUpstreamConfig](upstreamCfg)
}

func (m *upstreamConfigMapper) DistUpstreamToUpstream(cfg DistUpstreamConfig) (UpstreamConfig, error) {
	upstreamCfg, err := m.mapUpstreamConfig[UpstreamConfig](cfg)
	if err != nil {
		return UpstreamConfig{}, err
	}
	upstreamCfg.Type = ecosystemDist
	return upstreamCfg, nil
}

func (m *upstreamConfigMapper) UpstreamToDistUpstream(upstreamCfg UpstreamConfig) (DistUpstreamConfig, error) {
	return m.mapUpstreamConfig[DistUpstreamConfig](upstreamCfg)
}

func (m *upstreamConfigMapper) mapUpstreamConfig[D, S any](src S) (D, error) {
	if m == nil || m.mapper == nil {
		m = newUpstreamConfigMapper()
	}
	dst, err := m.mapper.Map[D](src)
	if err != nil {
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
