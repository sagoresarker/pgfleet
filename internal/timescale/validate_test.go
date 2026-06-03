package timescale

import "testing"

func TestValidateIdent_Accepts(t *testing.T) {
	valid := []string{
		"a",
		"_",
		"_x",
		"metrics",
		"my_table",
		"Table1",
		"MixedCase_99",
		"x123",
		"select", // SQL keyword as text is fine format-wise
		"timescaledb",
		"pg_class",
		"a23456789012345678901234567890123456789012345678901234567890123", // 63 chars
	}
	for _, name := range valid {
		if err := ValidateIdent(name); err != nil {
			t.Errorf("ValidateIdent(%q) = %v, want nil", name, err)
		}
	}
}

func TestValidateIdent_Rejects(t *testing.T) {
	invalid := []string{
		"",
		" ",
		"1a",
		"9",
		"a b",
		"a-b",
		"a.b",
		"a;DROP",
		"a;DROP TABLE x",
		`a"b`,
		"a'b",
		"a`b",
		"a\tb",
		"a\nb",
		"a\x00b",
		"café",    // unicode
		"naïve",   // unicode
		"таблица", // cyrillic
		"a2345678901234567890123456789012345678901234567890123456789012345", // 64 chars
		"$a",
		"a$",
		"(a)",
		"a,b",
		"a/*x*/",
		"--comment",
	}
	for _, name := range invalid {
		if err := ValidateIdent(name); err == nil {
			t.Errorf("ValidateIdent(%q) = nil, want error", name)
		}
	}
}

func TestValidateQualifiedName_Accepts(t *testing.T) {
	valid := []string{
		"metrics",        // unqualified
		"public.metrics", // schema.table
		"_s._t",
		"Schema1.Table2",
	}
	for _, name := range valid {
		if err := ValidateQualifiedName(name); err != nil {
			t.Errorf("ValidateQualifiedName(%q) = %v, want nil", name, err)
		}
	}
}

func TestValidateQualifiedName_Rejects(t *testing.T) {
	invalid := []string{
		"",
		".",
		"a.",
		".a",
		"a.b.c", // too many parts
		"a.b;DROP",
		"a b.c",
		"1a.b",
		"a.2b",
		`a."b"`,
		"a.b.c.d",
	}
	for _, name := range invalid {
		if err := ValidateQualifiedName(name); err == nil {
			t.Errorf("ValidateQualifiedName(%q) = nil, want error", name)
		}
	}
}

func TestValidateInterval_Accepts(t *testing.T) {
	valid := []string{
		"7 days",
		"30 days",
		"1 day",
		"12 hours",
		"1 hour 30 minutes",
		"90 minutes",
		"6 months",
		"1 year",
		"24h",
	}
	for _, v := range valid {
		if err := ValidateInterval(v); err != nil {
			t.Errorf("ValidateInterval(%q) = %v, want nil", v, err)
		}
	}
}

func TestValidateInterval_Rejects(t *testing.T) {
	invalid := []string{
		"",
		"   ",
		"7 days'",
		"7 days; DROP TABLE x",
		"7 days)",
		`7 "days"`,
		"7--days",
		"7/*x*/days",
		"$$x$$",
		"7\ndays",
		"7\x00days",
	}
	for _, v := range invalid {
		if err := ValidateInterval(v); err == nil {
			t.Errorf("ValidateInterval(%q) = nil, want error", v)
		}
	}
}

func TestValidateIdentList_Accepts(t *testing.T) {
	valid := []string{
		"device_id",
		"device_id, ts",
		"a,b,c",
		" a , b ",
	}
	for _, v := range valid {
		if err := ValidateIdentList(v); err != nil {
			t.Errorf("ValidateIdentList(%q) = %v, want nil", v, err)
		}
	}
}

func TestValidateIdentList_Rejects(t *testing.T) {
	invalid := []string{
		"",
		",",
		"a,",
		",a",
		"a,,b",
		"a;DROP, b",
		"a b, c",
		`a, "b"`,
		"1a, b",
	}
	for _, v := range invalid {
		if err := ValidateIdentList(v); err == nil {
			t.Errorf("ValidateIdentList(%q) = nil, want error", v)
		}
	}
}
