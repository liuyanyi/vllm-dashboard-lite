package main

import (
	"fmt"
	"io"
	"regexp"

	"gopkg.in/yaml.v3"
)

var endpointID = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$`)

// YAML is the sole source of endpoints.
// The database remains available for observation sessions and rollups only.
func loadStaticConfig(r io.Reader) ([]Config, error) {
	var file struct {
		Endpoints []Config `yaml:"endpoints"`
	}
	d := yaml.NewDecoder(r)
	d.KnownFields(true)
	if err := d.Decode(&file); err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("config: expected exactly one YAML document")
	}
	if file.Endpoints == nil {
		return nil, fmt.Errorf("config: endpoints must be specified (use [] for an empty list)")
	}
	ids, urls := map[string]bool{}, map[string]bool{}
	for i := range file.Endpoints {
		c := &file.Endpoints[i]
		if !endpointID.MatchString(c.ID) {
			return nil, fmt.Errorf("config: endpoint %d requires a stable alphanumeric id (also . _ -)", i+1)
		}
		if err := valid(c); err != nil {
			return nil, fmt.Errorf("config: endpoint %s: %w", c.ID, err)
		}
		if ids[c.ID] {
			return nil, fmt.Errorf("config: duplicate id %s", c.ID)
		}
		key := c.URL + c.MetricsPath
		if urls[key] {
			return nil, fmt.Errorf("config: duplicate metrics endpoint for %s", c.ID)
		}
		ids[c.ID], urls[key] = true, true
	}
	return file.Endpoints, nil
}
