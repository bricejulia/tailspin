package gcplog

import (
	"reflect"
	"testing"

	"cloud.google.com/go/logging"
)

func TestBuildFacetsEmpty(t *testing.T) {
	got := BuildFacets(nil)
	want := Facets{}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("BuildFacets(nil) = %+v, want %+v", got, want)
	}
}

func TestBuildFacetsCountsAndSortOrder(t *testing.T) {
	entries := []Entry{
		{Severity: logging.Warning, LogName: "syslog", Resource: "gce_instance", Labels: map[string]string{"env": "prod"}},
		{Severity: logging.Warning, LogName: "syslog", Resource: "gce_instance", Labels: map[string]string{"env": "prod"}},
		{Severity: logging.Error, LogName: "app", Resource: "k8s_container", Labels: map[string]string{"env": "staging"}},
		{Severity: logging.Default}, // the zero value must still be bucketed, not dropped
	}

	got := BuildFacets(entries)

	wantSeverity := []SeverityFacetCount{
		{Severity: logging.Warning, Count: 2},
		{Severity: logging.Default, Count: 1},
		{Severity: logging.Error, Count: 1},
	}
	if !reflect.DeepEqual(got.Severity, wantSeverity) {
		t.Errorf("Severity = %+v, want %+v", got.Severity, wantSeverity)
	}

	wantLogName := []FacetCount{{Value: "syslog", Count: 2}, {Value: "app", Count: 1}}
	if !reflect.DeepEqual(got.LogName, wantLogName) {
		t.Errorf("LogName = %+v, want %+v", got.LogName, wantLogName)
	}

	wantResource := []FacetCount{{Value: "gce_instance", Count: 2}, {Value: "k8s_container", Count: 1}}
	if !reflect.DeepEqual(got.Resource, wantResource) {
		t.Errorf("Resource = %+v, want %+v", got.Resource, wantResource)
	}

	wantLabels := []LabelFacet{
		{Key: "env", Values: []FacetCount{{Value: "prod", Count: 2}, {Value: "staging", Count: 1}}},
	}
	if !reflect.DeepEqual(got.Labels, wantLabels) {
		t.Errorf("Labels = %+v, want %+v", got.Labels, wantLabels)
	}
}

func TestBuildFacetsSkipsEmptyLogNameAndResource(t *testing.T) {
	got := BuildFacets([]Entry{{Severity: logging.Info}})
	if got.LogName != nil {
		t.Errorf("LogName = %+v, want nil (empty LogName shouldn't be a facet row)", got.LogName)
	}
	if got.Resource != nil {
		t.Errorf("Resource = %+v, want nil (empty Resource shouldn't be a facet row)", got.Resource)
	}
}

func TestBuildFacetsMultipleLabelKeysSortedByKey(t *testing.T) {
	entries := []Entry{
		{Labels: map[string]string{"zone": "us-east1", "env": "prod"}},
		{Labels: map[string]string{"zone": "us-west1"}},
	}
	got := BuildFacets(entries)
	if len(got.Labels) != 2 {
		t.Fatalf("len(Labels) = %d, want 2", len(got.Labels))
	}
	if got.Labels[0].Key != "env" || got.Labels[1].Key != "zone" {
		t.Errorf("Labels keys = [%q, %q], want [env, zone] (sorted)", got.Labels[0].Key, got.Labels[1].Key)
	}
}

// TestFacetAggregatorMergeMatchesBatch proves the concurrent-sub-range
// fetch path (which builds one FacetAggregator per goroutine and Merges
// them) is equivalent to a single batch BuildFacets call over the
// concatenated entries — the property client.Facets' correctness rests on.
func TestFacetAggregatorMergeMatchesBatch(t *testing.T) {
	group1 := []Entry{
		{Severity: logging.Warning, LogName: "a", Labels: map[string]string{"k": "v1"}},
		{Severity: logging.Error, LogName: "b"},
	}
	group2 := []Entry{
		{Severity: logging.Warning, LogName: "a", Labels: map[string]string{"k": "v2"}},
		{Severity: logging.Default, Resource: "gce_instance"},
	}

	var a, b FacetAggregator
	a.AddAll(group1)
	b.AddAll(group2)
	a.Merge(b)
	merged := a.Result()

	batch := BuildFacets(append(append([]Entry{}, group1...), group2...))

	if !reflect.DeepEqual(merged, batch) {
		t.Errorf("merged aggregators = %+v, want %+v (equal to one batch call)", merged, batch)
	}
}

func TestFacetAggregatorMergeIntoEmpty(t *testing.T) {
	var a FacetAggregator
	var b FacetAggregator
	b.AddAll([]Entry{{Severity: logging.Info, LogName: "x"}})
	a.Merge(b)
	if got, want := a.Result(), b.Result(); !reflect.DeepEqual(got, want) {
		t.Errorf("Merge into empty = %+v, want %+v", got, want)
	}
}
