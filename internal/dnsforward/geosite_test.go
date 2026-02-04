package dnsforward

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseGeositeRule(t *testing.T) {
	testCases := []struct {
		name          string
		line          string
		wantCategory  string
		wantUpstreams []string
		wantOK        bool
	}{{
		name:          "valid_single_upstream",
		line:          "[geosite:cn]114.114.114.114",
		wantCategory:  "cn",
		wantUpstreams: []string{"114.114.114.114"},
		wantOK:        true,
	}, {
		name:          "valid_multiple_upstreams",
		line:          "[geosite:google]8.8.8.8 8.8.4.4",
		wantCategory:  "google",
		wantUpstreams: []string{"8.8.8.8", "8.8.4.4"},
		wantOK:        true,
	}, {
		name:          "valid_with_whitespace",
		line:          "  [geosite:netflix]  1.1.1.1  ",
		wantCategory:  "netflix",
		wantUpstreams: []string{"1.1.1.1"},
		wantOK:        true,
	}, {
		name:          "not_geosite_rule",
		line:          "[/example.com/]8.8.8.8",
		wantCategory:  "",
		wantUpstreams: nil,
		wantOK:        false,
	}, {
		name:          "missing_closing_bracket",
		line:          "[geosite:cn 8.8.8.8",
		wantCategory:  "",
		wantUpstreams: nil,
		wantOK:        false,
	}, {
		name:          "empty_category",
		line:          "[geosite:]8.8.8.8",
		wantCategory:  "",
		wantUpstreams: nil,
		wantOK:        false,
	}, {
		name:          "missing_upstream",
		line:          "[geosite:cn]",
		wantCategory:  "",
		wantUpstreams: nil,
		wantOK:        false,
	}, {
		name:          "empty_line",
		line:          "",
		wantCategory:  "",
		wantUpstreams: nil,
		wantOK:        false,
	}}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			category, upstreams, ok := parseGeositeRule(tc.line)

			assert.Equal(t, tc.wantOK, ok)
			assert.Equal(t, tc.wantCategory, category)
			assert.Equal(t, tc.wantUpstreams, upstreams)
		})
	}
}

func TestGeositeManager_FilterGeositeRules(t *testing.T) {
	gm := &geositeManager{}

	upstreams := []string{
		"8.8.8.8",
		"[geosite:cn]114.114.114.114",
		"[/example.com/]1.1.1.1",
		"[geosite:google]8.8.8.8 8.8.4.4",
		"1.1.1.1",
	}

	filtered := gm.filterGeositeRules(upstreams)

	expected := []string{
		"8.8.8.8",
		"[/example.com/]1.1.1.1",
		"1.1.1.1",
	}

	assert.Equal(t, expected, filtered)
}

func TestGeositeManager_SetRules(t *testing.T) {
	gm := &geositeManager{
		rules:  nil,
		logger: slog.Default(),
	}

	upstreams := []string{
		"8.8.8.8",
		"[geosite:cn]114.114.114.114 223.5.5.5",
		"[/example.com/]1.1.1.1",
		"[geosite:google]8.8.8.8",
	}

	gm.setRules(upstreams)

	require.Len(t, gm.rules, 2)

	assert.Equal(t, "cn", gm.rules[0].category)
	assert.Equal(t, []string{"114.114.114.114", "223.5.5.5"}, gm.rules[0].upstreams)

	assert.Equal(t, "google", gm.rules[1].category)
	assert.Equal(t, []string{"8.8.8.8"}, gm.rules[1].upstreams)
}
