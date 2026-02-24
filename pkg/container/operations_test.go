package container

import "testing"

func TestValidateDomain(t *testing.T) {
	valid := []string{
		"example.com",
		"sub.example.com",
		"a.b.c.example.com",
		"example-site.com",
		"*.example.com",
		"a.co",
		"x",
	}
	for _, d := range valid {
		if err := ValidateDomain(d); err != nil {
			t.Errorf("ValidateDomain(%q) should be valid, got: %v", d, err)
		}
	}

	invalid := []struct {
		domain string
		reason string
	}{
		{"", "empty"},
		{".example.com", "leading dot"},
		{"example.com.", "trailing dot"},
		{"example..com", "consecutive dots"},
		{"exam ple.com", "space"},
		{"exam\tple.com", "tab"},
		{"example.com;rm -rf /", "semicolon injection"},
		{"example.com'evil", "single quote injection"},
		{"example.com$(cmd)", "dollar injection"},
		{"example.com`cmd`", "backtick injection"},
		{"-example.com", "label starts with hyphen"},
		{"example-.com", "label ends with hyphen"},
		{"*", "bare wildcard without domain"},
		{"foo.*.com", "wildcard not in first label"},
		{"*.*.com", "multiple wildcards"},
		{"a*b.example.com", "partial wildcard in label"},
		{"example.*", "wildcard in last label"},
	}
	for _, tc := range invalid {
		if err := ValidateDomain(tc.domain); err == nil {
			t.Errorf("ValidateDomain(%q) should be invalid (%s)", tc.domain, tc.reason)
		}
	}
}

func TestValidateIP(t *testing.T) {
	valid := []string{
		"1.2.3.4",
		"10.0.0.1",
		"255.255.255.255",
		"::1",
		"fe80::1",
		"2001:db8::1",
	}
	for _, ip := range valid {
		if err := ValidateIP(ip); err != nil {
			t.Errorf("ValidateIP(%q) should be valid, got: %v", ip, err)
		}
	}

	invalid := []struct {
		ip     string
		reason string
	}{
		{"", "empty"},
		{"not-an-ip", "not an IP"},
		{"1.2.3.4;rm -rf /", "injection"},
		{"1.2.3.4 && evil", "command chaining"},
		{"999.999.999.999", "out of range"},
	}
	for _, tc := range invalid {
		if err := ValidateIP(tc.ip); err == nil {
			t.Errorf("ValidateIP(%q) should be invalid (%s)", tc.ip, tc.reason)
		}
	}
}
