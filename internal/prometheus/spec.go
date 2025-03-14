package prometheus

import (
	"context"
	"fmt"
	"time"

	"gopkg.in/yaml.v2"

	prometheusv1 "github.com/slok/sloth/pkg/prometheus/api/v1"
	prometheuspluginv1 "github.com/slok/sloth/pkg/prometheus/plugin/v1"
)

// YAMLSpecLoader knows how to load YAML specs and converts them to a model.
type YAMLSpecLoader struct {
	plugins map[string]SLIPlugin
}

// NewYAMLSpecLoader returns a YAML spec loader.
func NewYAMLSpecLoader(plugins map[string]SLIPlugin) YAMLSpecLoader {
	return YAMLSpecLoader{
		plugins: plugins,
	}
}

func (y YAMLSpecLoader) LoadSpec(ctx context.Context, data []byte) (*SLOGroup, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("spec is required")
	}

	s := prometheusv1.Spec{}
	err := yaml.Unmarshal(data, &s)
	if err != nil {
		return nil, fmt.Errorf("could not unmarshall YAML spec correctly: %w", err)
	}

	// Check version.
	if s.Version != prometheusv1.Version {
		return nil, fmt.Errorf("invalid spec version, should be %q", prometheusv1.Version)
	}

	// Check at least we have one SLO.
	if len(s.SLOs) == 0 {
		return nil, fmt.Errorf("at least one SLO is required")
	}

	m, err := y.mapSpecToModel(ctx, s)
	if err != nil {
		return nil, fmt.Errorf("could not map to model: %w", err)
	}

	return m, nil
}

func (y YAMLSpecLoader) mapSpecToModel(ctx context.Context, spec prometheusv1.Spec) (*SLOGroup, error) {
	models := make([]SLO, 0, len(spec.SLOs))
	for _, specSLO := range spec.SLOs {
		slo := SLO{
			ID:              fmt.Sprintf("%s-%s", spec.Service, specSLO.Name),
			Name:            specSLO.Name,
			Description:     specSLO.Description,
			Service:         spec.Service,
			TimeWindow:      30 * 24 * time.Hour, // Default and for now the only one supported.
			Objective:       specSLO.Objective,
			Labels:          mergeLabels(spec.Labels, specSLO.Labels),
			PageAlertMeta:   AlertMeta{Disable: true},
			TicketAlertMeta: AlertMeta{Disable: true},
		}

		// Set SLIs.
		if specSLO.SLI.Events != nil {
			slo.SLI.Events = &SLIEvents{
				ErrorQuery: specSLO.SLI.Events.ErrorQuery,
				TotalQuery: specSLO.SLI.Events.TotalQuery,
			}
		}

		if specSLO.SLI.Raw != nil {
			slo.SLI.Raw = &SLIRaw{
				ErrorRatioQuery: specSLO.SLI.Raw.ErrorRatioQuery,
			}
		}

		if specSLO.SLI.Plugin != nil {
			plugin, ok := y.plugins[specSLO.SLI.Plugin.ID]
			if !ok {
				return nil, fmt.Errorf("unknown plugin: %q", specSLO.SLI.Plugin.ID)
			}

			meta := map[string]string{
				prometheuspluginv1.SLIPluginMetaService:   spec.Service,
				prometheuspluginv1.SLIPluginMetaSLO:       specSLO.Name,
				prometheuspluginv1.SLIPluginMetaObjective: fmt.Sprintf("%f", specSLO.Objective),
			}

			rawQuery, err := plugin.Func(ctx, meta, spec.Labels, specSLO.SLI.Plugin.Options)
			if err != nil {
				return nil, fmt.Errorf("plugin %q execution error: %w", specSLO.SLI.Plugin.ID, err)
			}

			slo.SLI.Raw = &SLIRaw{
				ErrorRatioQuery: rawQuery,
			}
		}

		// Set alerts.
		if !specSLO.Alerting.PageAlert.Disable {
			slo.PageAlertMeta = AlertMeta{
				Name:        specSLO.Alerting.Name,
				Labels:      mergeLabels(specSLO.Alerting.Labels, specSLO.Alerting.PageAlert.Labels),
				Annotations: mergeLabels(specSLO.Alerting.Annotations, specSLO.Alerting.PageAlert.Annotations),
			}
		}

		if !specSLO.Alerting.TicketAlert.Disable {
			slo.TicketAlertMeta = AlertMeta{
				Name:        specSLO.Alerting.Name,
				Labels:      mergeLabels(specSLO.Alerting.Labels, specSLO.Alerting.TicketAlert.Labels),
				Annotations: mergeLabels(specSLO.Alerting.Annotations, specSLO.Alerting.TicketAlert.Annotations),
			}
		}

		models = append(models, slo)
	}

	return &SLOGroup{SLOs: models}, nil
}
