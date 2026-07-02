package repository

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestParseIPAPI_Timezone_Preserved pins the contract that a successful ip-api
// response carrying a valid non-CN IANA timezone surfaces that timezone on the
// parsed ProxyExitInfo, so the resolver can write it into OAuth profiles.
func TestParseIPAPI_Timezone_Preserved(t *testing.T) {
	body := []byte(`{"status":"success","query":"203.0.113.7","city":"Los Angeles","regionName":"California","country":"United States","countryCode":"US","timezone":"America/Los_Angeles"}`)
	info, _, err := (&proxyProbeService{}).parseIPAPI(body, 100)
	require.NoError(t, err)
	require.Equal(t, "203.0.113.7", info.IP)
	require.Equal(t, "US", info.CountryCode)
	require.Equal(t, "America/Los_Angeles", info.Timezone)
	require.Equal(t, "ip-api", info.Source)
}

// TestParseIPAPI_Timezone_CN_CountryCode_NormalizesToFallback asserts that a
// CN country code forces the parsed timezone to the US fallback, even when
// ip-api returns a syntactically valid IANA timezone like Asia/Shanghai.
func TestParseIPAPI_Timezone_CN_CountryCode_NormalizesToFallback(t *testing.T) {
	body := []byte(`{"status":"success","query":"1.2.3.4","city":"Beijing","regionName":"Beijing","country":"China","countryCode":"CN","timezone":"Asia/Shanghai"}`)
	info, _, err := (&proxyProbeService{}).parseIPAPI(body, 100)
	require.NoError(t, err)
	require.Equal(t, "CN", info.CountryCode)
	require.Equal(t, "America/Los_Angeles", info.Timezone, "CN country code must normalize to US fallback")
}

// TestParseIPAPI_Timezone_AsiaShanghai_NormalizesToFallback asserts that a
// China timezone name normalizes to the US fallback even when the country code
// is missing or non-CN, defending the no-China-timezone invariant.
func TestParseIPAPI_Timezone_AsiaShanghai_NormalizesToFallback(t *testing.T) {
	body := []byte(`{"status":"success","query":"1.2.3.4","city":"Shanghai","regionName":"Shanghai","country":"","countryCode":"","timezone":"Asia/Shanghai"}`)
	info, _, err := (&proxyProbeService{}).parseIPAPI(body, 100)
	require.NoError(t, err)
	require.Equal(t, "America/Los_Angeles", info.Timezone, "Asia/Shanghai must normalize to US fallback regardless of country code")
}

// TestParseIPAPI_Timezone_Missing_NormalizesToFallback asserts that an
// otherwise successful ip-api response with no timezone field yields the US
// fallback rather than an empty string.
func TestParseIPAPI_Timezone_Missing_NormalizesToFallback(t *testing.T) {
	body := []byte(`{"status":"success","query":"5.6.7.8","city":"x","regionName":"y","country":"z","countryCode":"ZZ"}`)
	info, _, err := (&proxyProbeService{}).parseIPAPI(body, 100)
	require.NoError(t, err)
	require.Equal(t, "America/Los_Angeles", info.Timezone, "missing timezone must normalize to US fallback")
}

// TestParseIPAPI_Timezone_Invalid_NormalizesToFallback asserts that a
// successful ip-api response with a non-IANA timezone string yields the US
// fallback rather than propagating an unusable value.
func TestParseIPAPI_Timezone_Invalid_NormalizesToFallback(t *testing.T) {
	body := []byte(`{"status":"success","query":"5.6.7.8","city":"x","regionName":"y","country":"z","countryCode":"ZZ","timezone":"Not/A/Real/Zone"}`)
	info, _, err := (&proxyProbeService{}).parseIPAPI(body, 100)
	require.NoError(t, err)
	require.Equal(t, "America/Los_Angeles", info.Timezone, "invalid timezone must normalize to US fallback")
}

// TestParseIPAPI_StatusFail_ReturnsError pins the contract that a
// status=fail response is a probe error, never a partial success.
func TestParseIPAPI_StatusFail_ReturnsError(t *testing.T) {
	body := []byte(`{"status":"fail","message":"rate limited"}`)
	_, _, err := (&proxyProbeService{}).parseIPAPI(body, 100)
	require.Error(t, err)
	require.ErrorContains(t, err, "rate limited")
}

// TestParseHTTPBin_YieldsNoTimezone pins that the IP-only fallback source
// never satisfies profile timezone resolution: its parsed result carries an
// empty timezone, so the resolver must fall back to America/Los_Angeles.
func TestParseHTTPBin_YieldsNoTimezone(t *testing.T) {
	body := []byte(`{"origin": "9.8.7.6"}`)
	info, _, err := (&proxyProbeService{}).parseHTTPBin(body, 50)
	require.NoError(t, err)
	require.Equal(t, "9.8.7.6", info.IP)
	require.Empty(t, info.Timezone, "httpbin is IP-only and must never carry a timezone")
	require.Equal(t, "httpbin", info.Source)
}
