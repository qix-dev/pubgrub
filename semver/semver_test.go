// SPDX-License-Identifier: MPL-2.0
package semver_test

import (
	"encoding/json"
	"pubgrub-go/semver"
	"testing"
)

func TestVersionPrecedence(t *testing.T) {
	names := []string{"1.0.0-alpha", "1.0.0-alpha.1", "1.0.0-alpha.beta", "1.0.0-beta", "1.0.0-beta.2", "1.0.0-beta.11", "1.0.0-rc.1", "1.0.0"}
	var previous semver.Version
	for i, name := range names {
		v, err := semver.Parse(name)
		if err != nil {
			t.Fatal(err)
		}
		if i > 0 && semver.ComparePrecedence(previous, v) >= 0 {
			t.Fatalf("%s >= %s", previous, v)
		}
		previous = v
	}
}
func TestRequirementConversion(t *testing.T) {
	cases := []struct{ input, want string }{{"*", "*"}, {"1.2.3", ">=1.2.3, <2.0.0"}, {"^0.2.3", ">=0.2.3, <0.3.0"}, {"^0.0.3", "0.0.3"}, {"^0.0", ">=0.0.0, <0.1.0"}, {"=1", ">=1.0.0, <2.0.0"}, {"=1.2", ">=1.2.0, <1.3.0"}, {"=1.2.3", "1.2.3"}, {">1", ">=2.0.0"}, {">1.2", ">=1.3.0"}, {">1.2.3", ">1.2.3"}, {">=1.2", ">=1.2.0"}, {"<2", "<2.0.0"}, {"<=1.2", "<1.3.0"}, {"<=1.2.3", "<=1.2.3"}, {"~1.2.3", ">=1.2.3, <1.3.0"}, {"1.*", ">=1.0.0, <2.0.0"}, {"1.2.*", ">=1.2.0, <1.3.0"}, {">=1, <2", ">=1.0.0, <2.0.0"}, {"=1.2.3-beta", "1.2.3"}}
	for _, c := range cases {
		t.Run(c.input, func(t *testing.T) {
			r, err := semver.ParseRanges(c.input)
			if err != nil {
				t.Fatal(err)
			}
			if r.String() != c.want {
				t.Fatalf("got %s want %s", r, c.want)
			}
		})
	}
}
func TestVersionJSON(t *testing.T) {
	v, err := semver.Parse("1.2.3-rc.2+build.5")
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var restored semver.Version
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}
	if restored != v {
		t.Fatal(restored)
	}
}
func TestInvalidVersion(t *testing.T) {
	if _, err := semver.Parse("1.2.3-01"); err == nil {
		t.Fatal("leading zero prerelease accepted")
	}
}
func TestInvalidRequirement(t *testing.T) {
	if _, err := semver.ParseRanges("1.*.3"); err == nil {
		t.Fatal("numeric component after wildcard accepted")
	}
}
func TestVersionText(t *testing.T) {
	cases := []struct {
		version semver.Version
		text    string
	}{
		{semver.Version{}, "0.0.0"},
		{semver.New(1, 2, 3), "1.2.3"},
		{semver.Version{Major: 1, Minor: 2, Patch: 3, Pre: "rc.2"}, "1.2.3-rc.2"},
		{semver.Version{Major: 1, Minor: 2, Patch: 3, Build: "build.005"}, "1.2.3+build.005"},
		{semver.Version{Major: 1, Minor: 2, Patch: 3, Pre: "rc.2", Build: "build.005"}, "1.2.3-rc.2+build.005"},
		{semver.Version{Major: ^uint64(0)}, "18446744073709551615.0.0"},
	}
	for _, c := range cases {
		t.Run(c.text, func(t *testing.T) {
			data, err := c.version.MarshalText()
			if err != nil || string(data) != c.text {
				t.Fatalf("MarshalText() = %q, %v; want %q", data, err, c.text)
			}
			restored := semver.Version{Major: 9, Minor: 8, Patch: 7, Pre: "old", Build: "old"}
			if err := restored.UnmarshalText(data); err != nil || restored != c.version {
				t.Fatalf("UnmarshalText(%q) = %v, %v; want %v", data, restored, err, c.version)
			}
		})
	}
	for _, text := range []string{"1.2", "1.2.3-01", "1.2.3+bad!", "01.2.3", "18446744073709551616.0.0"} {
		t.Run("invalid/"+text, func(t *testing.T) {
			before := semver.Version{Major: 9, Minor: 8, Patch: 7, Pre: "old", Build: "old"}
			got := before
			if err := got.UnmarshalText([]byte(text)); err == nil {
				t.Fatal("invalid version accepted")
			}
			if got != before {
				t.Fatalf("failed decode changed receiver: got %v; want %v", got, before)
			}
		})
	}
}
