package pilot

import "encoding/json"

// MarshalJSON preserves unknown estimates as null. encoding/json's default
// handling of an empty json.Number would turn absent measurements into zero.
func (r MetricResult) MarshalJSON() ([]byte, error) {
	type plain MetricResult
	raw, err := json.Marshal(plain(r))
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	for key, value := range map[string]json.Number{"estimate": r.Estimate, "lower": r.Lower, "upper": r.Upper} {
		if value == "" {
			fields[key] = json.RawMessage("null")
		}
	}
	return json.Marshal(fields)
}
