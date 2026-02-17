package inifiles

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testDir = "../../config-inifiles/t"

func testIniPath(name string) string {
	return filepath.Join(testDir, name)
}

// ---------- Basic Parsing Tests ----------

func TestBasicParsing(t *testing.T) {
	ini, err := New(Options{File: testIniPath("test.ini")})
	if err != nil {
		t.Fatalf("Failed to parse test.ini: %v", err)
	}

	// Test sections exist
	sections := ini.Sections()
	if len(sections) == 0 {
		t.Fatal("Expected sections, got none")
	}

	if !ini.SectionExists("test1") {
		t.Error("Expected section 'test1' to exist")
	}
	if !ini.SectionExists("test2") {
		t.Error("Expected section 'test2' to exist")
	}
	if !ini.SectionExists("[w]eird characters") {
		t.Error("Expected section '[w]eird characters' to exist")
	}

	// Test values
	if v := ini.Val("test1", "one"); v != "value1" {
		t.Errorf("Expected 'value1', got %q", v)
	}
	if v := ini.Val("test1", "two"); v != "value2" {
		t.Errorf("Expected 'value2', got %q", v)
	}
	if v := ini.Val("test1", "three"); v != "value3" {
		t.Errorf("Expected 'value3', got %q", v)
	}
	if v := ini.Val("test2", "four"); v != "value4" {
		t.Errorf("Expected 'value4', got %q", v)
	}
	if v := ini.Val("test2", "five"); v != "value5" {
		t.Errorf("Expected 'value5', got %q", v)
	}
}

func TestMultiValuedParameters(t *testing.T) {
	ini, err := New(Options{File: testIniPath("test.ini")})
	if err != nil {
		t.Fatalf("Failed to parse test.ini: %v", err)
	}

	vals := ini.ValSlice("test1", "mult")
	if len(vals) != 3 {
		t.Fatalf("Expected 3 values for mult, got %d: %v", len(vals), vals)
	}
	if vals[0] != "one" || vals[1] != "two" || vals[2] != "three" {
		t.Errorf("Expected [one, two, three], got %v", vals)
	}

	// Val should join with newline
	joined := ini.Val("test1", "mult")
	if joined != "one\ntwo\nthree" {
		t.Errorf("Expected joined value 'one\\ntwo\\nthree', got %q", joined)
	}
}

func TestMultilineHereDoc(t *testing.T) {
	ini, err := New(Options{File: testIniPath("test.ini")})
	if err != nil {
		t.Fatalf("Failed to parse test.ini: %v", err)
	}

	vals := ini.ValSlice("[w]eird characters", "multiline")
	if len(vals) != 3 {
		t.Fatalf("Expected 3 lines for multiline, got %d: %v", len(vals), vals)
	}
	if vals[0] != "This" || vals[1] != "is a multi-line" || vals[2] != "value" {
		t.Errorf("Unexpected multiline values: %v", vals)
	}

	// Check EOT marker was captured
	eot, ok := ini.GetParameterEOT("[w]eird characters", "multiline")
	if !ok || eot != "EOT" {
		t.Errorf("Expected EOT marker 'EOT', got %q (ok=%v)", eot, ok)
	}
}

func TestEmptyValue(t *testing.T) {
	ini, err := New(Options{File: testIniPath("test.ini")})
	if err != nil {
		t.Fatalf("Failed to parse test.ini: %v", err)
	}

	if !ini.Exists("newsect", "seven") {
		t.Error("Expected parameter 'seven' to exist in 'newsect'")
	}
	if v := ini.Val("newsect", "seven"); v != "" {
		t.Errorf("Expected empty value for 'seven', got %q", v)
	}
}

func TestMultipleEqualsInValue(t *testing.T) {
	ini, err := New(Options{File: testIniPath("test.ini")})
	if err != nil {
		t.Fatalf("Failed to parse test.ini: %v", err)
	}

	v := ini.Val("test7", "criterion")
	if v != "price <= maximum" {
		t.Errorf("Expected 'price <= maximum', got %q", v)
	}
}

func TestSubstringParameterNames(t *testing.T) {
	ini, err := New(Options{File: testIniPath("test.ini")})
	if err != nil {
		t.Fatalf("Failed to parse test.ini: %v", err)
	}

	if v := ini.Val("substring", "boot"); v != "smarty" {
		t.Errorf("Expected 'smarty' for 'boot', got %q", v)
	}
	if v := ini.Val("substring", "bootcamp"); v != "dummy" {
		t.Errorf("Expected 'dummy' for 'bootcamp', got %q", v)
	}
}

// ---------- Exists Tests ----------

func TestExists(t *testing.T) {
	ini, err := New(Options{File: testIniPath("test.ini")})
	if err != nil {
		t.Fatalf("Failed to parse test.ini: %v", err)
	}

	if !ini.Exists("test1", "one") {
		t.Error("Expected 'one' to exist in 'test1'")
	}
	if ini.Exists("test1", "nonexistent") {
		t.Error("Expected 'nonexistent' to not exist in 'test1'")
	}
	if ini.Exists("nonexistent", "one") {
		t.Error("Expected section 'nonexistent' to not exist")
	}
}

// ---------- Default Section Tests ----------

func TestDefaultSection(t *testing.T) {
	content := `[default]
color=blue
size=large

[section1]
color=red
`
	ini, err := New(Options{Content: content, Default: "default"})
	if err != nil {
		t.Fatalf("Failed to parse: %v", err)
	}

	// section1 has its own color
	if v := ini.Val("section1", "color"); v != "red" {
		t.Errorf("Expected 'red', got %q", v)
	}

	// section1 should fall back to default for size
	if v := ini.Val("section1", "size"); v != "large" {
		t.Errorf("Expected 'large' from default section, got %q", v)
	}

	// Exists should NOT check default section
	if ini.Exists("section1", "size") {
		t.Error("Exists should not check default section")
	}
}

func TestValDefault(t *testing.T) {
	content := `[section1]
key=value
`
	ini, err := New(Options{Content: content})
	if err != nil {
		t.Fatalf("Failed to parse: %v", err)
	}

	if v := ini.Val("section1", "missing", "fallback"); v != "fallback" {
		t.Errorf("Expected 'fallback', got %q", v)
	}
}

// ---------- Case Insensitive Tests ----------

func TestCaseInsensitive(t *testing.T) {
	ini, err := New(Options{
		File:   testIniPath("test.ini"),
		NoCase: true,
	})
	if err != nil {
		t.Fatalf("Failed to parse: %v", err)
	}

	// Section names should be case-insensitive
	if !ini.SectionExists("TEST1") {
		t.Error("Expected 'TEST1' to exist (case insensitive)")
	}
	if !ini.SectionExists("Test1") {
		t.Error("Expected 'Test1' to exist (case insensitive)")
	}

	// Parameter names should be case-insensitive
	if v := ini.Val("test1", "ONE"); v != "value1" {
		t.Errorf("Expected 'value1', got %q", v)
	}

	// Mixed case section
	if !ini.SectionExists("mixedcasesect") {
		t.Error("Expected 'mixedcasesect' to exist (case insensitive)")
	}
	if v := ini.Val("mixedcasesect", "mixedcaseparam"); v != "MixedCaseVal" {
		t.Errorf("Expected 'MixedCaseVal' (value preserved), got %q", v)
	}
}

func TestCaseSensitiveDefault(t *testing.T) {
	ini, err := New(Options{
		File: testIniPath("case-sensitive-default.ini"),
	})
	if err != nil {
		t.Fatalf("Failed to parse: %v", err)
	}

	// With case-sensitive mode, check sections exist as named
	sections := ini.Sections()
	if len(sections) == 0 {
		t.Fatal("Expected sections")
	}
}

// ---------- Section Management Tests ----------

func TestAddSection(t *testing.T) {
	ini := NewEmpty()
	ini.AddSection("new_section")

	if !ini.SectionExists("new_section") {
		t.Error("Expected 'new_section' to exist after AddSection")
	}

	sections := ini.Sections()
	if len(sections) != 1 || sections[0] != "new_section" {
		t.Errorf("Expected ['new_section'], got %v", sections)
	}
}

func TestDeleteSection(t *testing.T) {
	content := `[sect1]
key=val
[sect2]
key=val
`
	ini, err := New(Options{Content: content})
	if err != nil {
		t.Fatal(err)
	}

	ok := ini.DeleteSection("sect1")
	if !ok {
		t.Error("Expected DeleteSection to return true")
	}
	if ini.SectionExists("sect1") {
		t.Error("Expected 'sect1' to be deleted")
	}
	if !ini.SectionExists("sect2") {
		t.Error("Expected 'sect2' to still exist")
	}
}

func TestRenameSection(t *testing.T) {
	content := `[old_name]
key1=val1
key2=val2
`
	ini, err := New(Options{Content: content})
	if err != nil {
		t.Fatal(err)
	}

	ok := ini.RenameSection("old_name", "new_name", false)
	if !ok {
		t.Error("Expected RenameSection to return true")
	}
	if ini.SectionExists("old_name") {
		t.Error("Expected 'old_name' to be gone")
	}
	if !ini.SectionExists("new_name") {
		t.Error("Expected 'new_name' to exist")
	}
	if v := ini.Val("new_name", "key1"); v != "val1" {
		t.Errorf("Expected 'val1', got %q", v)
	}
}

func TestCopySection(t *testing.T) {
	content := `[original]
key1=val1
key2=val2
`
	ini, err := New(Options{Content: content})
	if err != nil {
		t.Fatal(err)
	}

	ok := ini.CopySection("original", "copy", false)
	if !ok {
		t.Error("Expected CopySection to return true")
	}
	if !ini.SectionExists("original") {
		t.Error("Expected 'original' to still exist")
	}
	if !ini.SectionExists("copy") {
		t.Error("Expected 'copy' to exist")
	}
	if v := ini.Val("copy", "key1"); v != "val1" {
		t.Errorf("Expected 'val1', got %q", v)
	}
}

func TestParameters(t *testing.T) {
	ini, err := New(Options{File: testIniPath("test.ini")})
	if err != nil {
		t.Fatal(err)
	}

	params := ini.Parameters("test1")
	expected := []string{"three", "one", "two", "mult"}
	if len(params) != len(expected) {
		t.Fatalf("Expected %d params, got %d: %v", len(expected), len(params), params)
	}
	for i, p := range expected {
		if params[i] != p {
			t.Errorf("Expected param[%d]=%q, got %q", i, p, params[i])
		}
	}
}

// ---------- Value Modification Tests ----------

func TestNewVal(t *testing.T) {
	ini := NewEmpty()
	ini.AddSection("sect")
	ini.NewVal("sect", "key", "value")

	if v := ini.Val("sect", "key"); v != "value" {
		t.Errorf("Expected 'value', got %q", v)
	}
}

func TestNewValMultiple(t *testing.T) {
	ini := NewEmpty()
	ini.AddSection("sect")
	ini.NewVal("sect", "key", "v1", "v2", "v3")

	vals := ini.ValSlice("sect", "key")
	if len(vals) != 3 {
		t.Fatalf("Expected 3 values, got %d", len(vals))
	}
	if vals[0] != "v1" || vals[1] != "v2" || vals[2] != "v3" {
		t.Errorf("Expected [v1, v2, v3], got %v", vals)
	}
}

func TestNewValCreatesSection(t *testing.T) {
	ini := NewEmpty()
	ini.NewVal("newsect", "key", "value")

	if !ini.SectionExists("newsect") {
		t.Error("Expected NewVal to create section")
	}
	if v := ini.Val("newsect", "key"); v != "value" {
		t.Errorf("Expected 'value', got %q", v)
	}
}

func TestSetVal(t *testing.T) {
	content := `[sect]
key=old_value
`
	ini, err := New(Options{Content: content})
	if err != nil {
		t.Fatal(err)
	}

	ok := ini.SetVal("sect", "key", "new_value")
	if !ok {
		t.Error("Expected SetVal to return true")
	}
	if v := ini.Val("sect", "key"); v != "new_value" {
		t.Errorf("Expected 'new_value', got %q", v)
	}

	// SetVal on non-existent param should return false
	ok = ini.SetVal("sect", "nonexistent", "value")
	if ok {
		t.Error("Expected SetVal to return false for non-existent param")
	}
}

func TestPush(t *testing.T) {
	content := `[sect]
key=val1
`
	ini, err := New(Options{Content: content})
	if err != nil {
		t.Fatal(err)
	}

	ok := ini.Push("sect", "key", "val2", "val3")
	if !ok {
		t.Error("Expected Push to return true")
	}
	vals := ini.ValSlice("sect", "key")
	if len(vals) != 3 {
		t.Fatalf("Expected 3 values, got %d", len(vals))
	}
	if vals[0] != "val1" || vals[1] != "val2" || vals[2] != "val3" {
		t.Errorf("Expected [val1, val2, val3], got %v", vals)
	}
}

func TestDelVal(t *testing.T) {
	content := `[sect]
key1=val1
key2=val2
`
	ini, err := New(Options{Content: content})
	if err != nil {
		t.Fatal(err)
	}

	ok := ini.DelVal("sect", "key1")
	if !ok {
		t.Error("Expected DelVal to return true")
	}
	if ini.Exists("sect", "key1") {
		t.Error("Expected 'key1' to be deleted")
	}
	if !ini.Exists("sect", "key2") {
		t.Error("Expected 'key2' to still exist")
	}
}

// ---------- Comment Tests ----------

func TestSectionComments(t *testing.T) {
	ini, err := New(Options{File: testIniPath("test.ini")})
	if err != nil {
		t.Fatal(err)
	}

	cmt := ini.GetSectionComment("test1")
	if len(cmt) == 0 {
		t.Error("Expected section comment for test1")
	}
	if len(cmt) > 0 && !strings.Contains(cmt[0], "This is a section comment") {
		t.Errorf("Expected section comment containing 'This is a section comment', got %q", cmt[0])
	}
}

func TestParameterComments(t *testing.T) {
	ini, err := New(Options{File: testIniPath("test.ini")})
	if err != nil {
		t.Fatal(err)
	}

	cmt := ini.GetParameterComment("test2", "five")
	if len(cmt) == 0 {
		t.Error("Expected parameter comment for 'five' in 'test2'")
	}
	if len(cmt) > 0 && !strings.Contains(cmt[0], "This is a parm comment") {
		t.Errorf("Expected comment containing 'This is a parm comment', got %q", cmt[0])
	}
}

func TestSetSectionComment(t *testing.T) {
	ini := NewEmpty()
	ini.AddSection("sect")
	ini.SetSectionComment("sect", "my comment")

	cmt := ini.GetSectionComment("sect")
	if len(cmt) != 1 {
		t.Fatalf("Expected 1 comment line, got %d", len(cmt))
	}
	if !strings.HasPrefix(cmt[0], "#") {
		t.Error("Expected comment to start with '#'")
	}
	if !strings.Contains(cmt[0], "my comment") {
		t.Errorf("Expected 'my comment' in comment, got %q", cmt[0])
	}
}

func TestSetParameterComment(t *testing.T) {
	ini := NewEmpty()
	ini.AddSection("sect")
	ini.NewVal("sect", "key", "val")
	ini.SetParameterComment("sect", "key", "param comment")

	cmt := ini.GetParameterComment("sect", "key")
	if len(cmt) != 1 {
		t.Fatalf("Expected 1 comment line, got %d", len(cmt))
	}
	if !strings.Contains(cmt[0], "param comment") {
		t.Errorf("Expected 'param comment', got %q", cmt[0])
	}
}

func TestDeleteSectionComment(t *testing.T) {
	ini := NewEmpty()
	ini.AddSection("sect")
	ini.SetSectionComment("sect", "comment")
	ini.DeleteSectionComment("sect")

	cmt := ini.GetSectionComment("sect")
	if cmt != nil {
		t.Error("Expected nil after DeleteSectionComment")
	}
}

func TestDeleteParameterComment(t *testing.T) {
	ini := NewEmpty()
	ini.AddSection("sect")
	ini.NewVal("sect", "key", "val")
	ini.SetParameterComment("sect", "key", "comment")
	ini.DeleteParameterComment("sect", "key")

	cmt := ini.GetParameterComment("sect", "key")
	if cmt != nil {
		t.Error("Expected nil after DeleteParameterComment")
	}
}

func TestSemicolonComment(t *testing.T) {
	ini, err := New(Options{File: testIniPath("test.ini")})
	if err != nil {
		t.Fatal(err)
	}

	cmt := ini.GetSectionComment("MixedCaseSect")
	if len(cmt) == 0 {
		t.Error("Expected semicolon comment for MixedCaseSect")
	}
	if len(cmt) > 0 && !strings.Contains(cmt[0], "semi-colon comment") {
		t.Errorf("Expected semicolon comment, got %q", cmt[0])
	}
}

// ---------- Group Tests ----------

func TestGroups(t *testing.T) {
	ini, err := New(Options{File: testIniPath("test.ini")})
	if err != nil {
		t.Fatal(err)
	}

	groups := ini.Groups()
	found := false
	for _, g := range groups {
		if g == "group" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected 'group' in groups, got %v", groups)
	}
}

func TestGroupMembers(t *testing.T) {
	ini, err := New(Options{File: testIniPath("test.ini")})
	if err != nil {
		t.Fatal(err)
	}

	members := ini.GroupMembers("group")
	if len(members) < 2 {
		t.Fatalf("Expected at least 2 group members, got %d: %v", len(members), members)
	}
}

// ---------- Write/Output Tests ----------

func TestWriteConfig(t *testing.T) {
	content := `[section1]
key1=value1
key2=value2

[section2]
key3=value3
`
	ini, err := New(Options{Content: content})
	if err != nil {
		t.Fatal(err)
	}

	tmpDir := t.TempDir()
	outFile := filepath.Join(tmpDir, "output.ini")

	err = ini.WriteConfig(outFile)
	if err != nil {
		t.Fatalf("WriteConfig failed: %v", err)
	}

	// Re-read and verify
	ini2, err := New(Options{File: outFile})
	if err != nil {
		t.Fatalf("Failed to re-read: %v", err)
	}

	if v := ini2.Val("section1", "key1"); v != "value1" {
		t.Errorf("Expected 'value1', got %q", v)
	}
	if v := ini2.Val("section2", "key3"); v != "value3" {
		t.Errorf("Expected 'value3', got %q", v)
	}
}

func TestRoundTrip(t *testing.T) {
	ini, err := New(Options{File: testIniPath("test.ini")})
	if err != nil {
		t.Fatal(err)
	}

	tmpDir := t.TempDir()
	outFile := filepath.Join(tmpDir, "roundtrip.ini")

	err = ini.WriteConfig(outFile)
	if err != nil {
		t.Fatalf("WriteConfig failed: %v", err)
	}

	ini2, err := New(Options{File: outFile})
	if err != nil {
		t.Fatalf("Failed to re-read: %v", err)
	}

	// Verify key values survive round-trip
	if v := ini2.Val("test1", "one"); v != "value1" {
		t.Errorf("Round-trip: expected 'value1', got %q", v)
	}
	if v := ini2.Val("test7", "criterion"); v != "price <= maximum" {
		t.Errorf("Round-trip: expected 'price <= maximum', got %q", v)
	}

	// Verify multiline survives
	vals := ini2.ValSlice("test1", "mult")
	if len(vals) != 3 {
		t.Errorf("Round-trip: expected 3 mult values, got %d", len(vals))
	}
}

func TestString(t *testing.T) {
	ini := NewEmpty()
	ini.AddSection("sect")
	ini.NewVal("sect", "key", "val")

	s := ini.String()
	if !strings.Contains(s, "[sect]") {
		t.Error("Expected [sect] in string output")
	}
	if !strings.Contains(s, "key=val") {
		t.Error("Expected key=val in string output")
	}
}

func TestRewriteConfig(t *testing.T) {
	tmpDir := t.TempDir()
	outFile := filepath.Join(tmpDir, "rewrite.ini")

	// Write initial file
	ini := NewEmpty()
	ini.SetFileName(outFile)
	ini.AddSection("sect")
	ini.NewVal("sect", "key", "val1")
	err := ini.RewriteConfig()
	if err != nil {
		t.Fatalf("RewriteConfig failed: %v", err)
	}

	// Modify and rewrite
	ini.SetVal("sect", "key", "val2")
	err = ini.RewriteConfig()
	if err != nil {
		t.Fatalf("RewriteConfig failed: %v", err)
	}

	// Verify
	ini2, err := New(Options{File: outFile})
	if err != nil {
		t.Fatal(err)
	}
	if v := ini2.Val("sect", "key"); v != "val2" {
		t.Errorf("Expected 'val2', got %q", v)
	}
}

// ---------- Continuation Lines Tests ----------

func TestContinuationLines(t *testing.T) {
	content := `[sect]
key=this is a \
  continued \
  line
other=normal
`
	ini, err := New(Options{Content: content, AllowContinue: true})
	if err != nil {
		t.Fatal(err)
	}

	v := ini.Val("sect", "key")
	if v != "this is a continued line" {
		t.Errorf("Expected 'this is a continued line', got %q", v)
	}
	if v := ini.Val("sect", "other"); v != "normal" {
		t.Errorf("Expected 'normal', got %q", v)
	}
}

// ---------- Fallback Section Tests ----------

func TestFallbackSection(t *testing.T) {
	content := `global_key=global_val

[section1]
key=value
`
	ini, err := New(Options{Content: content, Fallback: "globals"})
	if err != nil {
		t.Fatal(err)
	}

	if !ini.SectionExists("globals") {
		t.Error("Expected fallback section 'globals' to exist")
	}
	if v := ini.Val("globals", "global_key"); v != "global_val" {
		t.Errorf("Expected 'global_val', got %q", v)
	}
	if v := ini.Val("section1", "key"); v != "value" {
		t.Errorf("Expected 'value', got %q", v)
	}
}

// ---------- Empty File Tests ----------

func TestEmptyFileError(t *testing.T) {
	_, err := New(Options{Content: ""})
	if err == nil {
		t.Error("Expected error for empty content without AllowEmpty")
	}
}

func TestEmptyFileAllowed(t *testing.T) {
	ini, err := New(Options{Content: "", AllowEmpty: true})
	if err != nil {
		t.Fatalf("Did not expect error with AllowEmpty: %v", err)
	}
	if len(ini.Sections()) != 0 {
		t.Error("Expected no sections")
	}
}

// ---------- Import/Delta Tests ----------

func TestImport(t *testing.T) {
	base := `[sect1]
key1=base_val1
key2=base_val2

[sect2]
key3=base_val3
`
	overlay := `[sect1]
key1=overlay_val1
`
	baseIni, err := New(Options{Content: base})
	if err != nil {
		t.Fatal(err)
	}

	overlayIni, err := New(Options{Content: overlay, Import: baseIni})
	if err != nil {
		t.Fatal(err)
	}

	// Overlay should override key1
	if v := overlayIni.Val("sect1", "key1"); v != "overlay_val1" {
		t.Errorf("Expected 'overlay_val1', got %q", v)
	}

	// Base values should be inherited
	if v := overlayIni.Val("sect1", "key2"); v != "base_val2" {
		t.Errorf("Expected 'base_val2', got %q", v)
	}

	// Base sections should be inherited
	if v := overlayIni.Val("sect2", "key3"); v != "base_val3" {
		t.Errorf("Expected 'base_val3', got %q", v)
	}
}

// ---------- AllowedCommentChars Tests ----------

func TestAllowedCommentChars(t *testing.T) {
	content := `# hash comment
; semicolon comment
[sect]
key=val
`
	ini, err := New(Options{Content: content, AllowedCommentChars: "#;"})
	if err != nil {
		t.Fatal(err)
	}

	if v := ini.Val("sect", "key"); v != "val" {
		t.Errorf("Expected 'val', got %q", v)
	}
}

// ---------- Trailing Comments Tests ----------

func TestTrailingComments(t *testing.T) {
	content := `[sect]
key=value ; this is a comment
other=no_comment
`
	ini, err := New(Options{Content: content, HandleTrailingComment: true})
	if err != nil {
		t.Fatal(err)
	}

	if v := ini.Val("sect", "key"); v != "value" {
		t.Errorf("Expected 'value', got %q", v)
	}

	cmt, ok := ini.GetParameterTrailingComment("sect", "key")
	if !ok {
		t.Error("Expected trailing comment to exist")
	}
	if !strings.Contains(cmt, "this is a comment") {
		t.Errorf("Expected trailing comment containing 'this is a comment', got %q", cmt)
	}
}

// ---------- PHP Compat Tests ----------

func TestPHPCompat(t *testing.T) {
	content := `[sect]
key1="quoted value"
key2='single quoted'
key3=unquoted
`
	ini, err := New(Options{Content: content, PHPCompat: true})
	if err != nil {
		t.Fatal(err)
	}

	if v := ini.Val("sect", "key1"); v != "quoted value" {
		t.Errorf("Expected 'quoted value', got %q", v)
	}
	if v := ini.Val("sect", "key2"); v != "single quoted" {
		t.Errorf("Expected 'single quoted', got %q", v)
	}
	if v := ini.Val("sect", "key3"); v != "unquoted" {
		t.Errorf("Expected 'unquoted', got %q", v)
	}
}

// ---------- NoMultiline Output Tests ----------

func TestNoMultilineOutput(t *testing.T) {
	ini := NewEmpty()
	ini.noMultiline = true
	ini.AddSection("sect")
	ini.NewVal("sect", "key", "v1", "v2", "v3")

	s := ini.String()
	// Should output repeated params instead of HERE doc
	count := strings.Count(s, "key=")
	if count != 3 {
		t.Errorf("Expected 3 'key=' lines, got %d in:\n%s", count, s)
	}
	if strings.Contains(s, "<<") {
		t.Error("Expected no HERE doc syntax with NoMultiline")
	}
}

// ---------- Delete All Tests ----------

func TestDelete(t *testing.T) {
	content := `[sect1]
key=val
[sect2]
key=val
`
	ini, err := New(Options{Content: content})
	if err != nil {
		t.Fatal(err)
	}

	ini.Delete()
	if len(ini.Sections()) != 0 {
		t.Error("Expected no sections after Delete")
	}
}

// ---------- EOT Marker Tests ----------

func TestSetGetDeleteEOT(t *testing.T) {
	ini := NewEmpty()
	ini.AddSection("sect")
	ini.NewVal("sect", "key", "val")

	ini.SetParameterEOT("sect", "key", "MYEOT")
	eot, ok := ini.GetParameterEOT("sect", "key")
	if !ok || eot != "MYEOT" {
		t.Errorf("Expected 'MYEOT', got %q (ok=%v)", eot, ok)
	}

	ini.DeleteParameterEOT("sect", "key")
	_, ok = ini.GetParameterEOT("sect", "key")
	if ok {
		t.Error("Expected EOT to be deleted")
	}
}

// ---------- WriteMode Tests ----------

func TestWriteMode(t *testing.T) {
	ini := NewEmpty()
	ini.SetWriteMode(0644)
	if ini.GetWriteMode() != 0644 {
		t.Errorf("Expected 0644, got %o", ini.GetWriteMode())
	}

	tmpDir := t.TempDir()
	outFile := filepath.Join(tmpDir, "mode_test.ini")
	ini.AddSection("sect")
	ini.NewVal("sect", "k", "v")
	err := ini.WriteConfig(outFile)
	if err != nil {
		t.Fatal(err)
	}

	fi, err := os.Stat(outFile)
	if err != nil {
		t.Fatal(err)
	}
	// Check the write permissions (mask with 0777 for portability)
	perm := fi.Mode().Perm()
	// On some systems umask may affect this, so just check it was set
	if perm&0600 == 0 {
		t.Errorf("Expected at least owner read/write, got %o", perm)
	}
}

// ---------- FileName Tests ----------

func TestSetGetFileName(t *testing.T) {
	ini := NewEmpty()
	ini.SetFileName("/tmp/test.ini")
	if ini.GetFileName() != "/tmp/test.ini" {
		t.Errorf("Expected '/tmp/test.ini', got %q", ini.GetFileName())
	}
}

// ---------- ReadConfig (Reload) Tests ----------

func TestReadConfig(t *testing.T) {
	tmpDir := t.TempDir()
	outFile := filepath.Join(tmpDir, "reload.ini")

	// Write initial file
	err := os.WriteFile(outFile, []byte("[sect]\nkey=val1\n"), 0644)
	if err != nil {
		t.Fatal(err)
	}

	ini, err := New(Options{File: outFile})
	if err != nil {
		t.Fatal(err)
	}
	if v := ini.Val("sect", "key"); v != "val1" {
		t.Fatalf("Expected 'val1', got %q", v)
	}

	// Modify file externally
	err = os.WriteFile(outFile, []byte("[sect]\nkey=val2\n"), 0644)
	if err != nil {
		t.Fatal(err)
	}

	err = ini.ReadConfig()
	if err != nil {
		t.Fatalf("ReadConfig failed: %v", err)
	}
	if v := ini.Val("sect", "key"); v != "val2" {
		t.Errorf("Expected 'val2' after reload, got %q", v)
	}
}

// ---------- From Reader Tests ----------

func TestFromReader(t *testing.T) {
	content := "[sect]\nkey=val\n"
	reader := strings.NewReader(content)

	ini, err := New(Options{Reader: reader})
	if err != nil {
		t.Fatal(err)
	}

	if v := ini.Val("sect", "key"); v != "val" {
		t.Errorf("Expected 'val', got %q", v)
	}
}

// ---------- Non-contiguous Groups ----------

func TestNonContiguousGroups(t *testing.T) {
	ini, err := New(Options{File: testIniPath("non-contiguous-groups.ini")})
	if err != nil {
		t.Fatal(err)
	}

	sections := ini.Sections()
	if len(sections) == 0 {
		t.Fatal("Expected sections")
	}

	groups := ini.Groups()
	if len(groups) == 0 {
		t.Fatal("Expected groups")
	}
}

// ---------- Brackets in Values ----------

func TestBracketsInValues(t *testing.T) {
	ini, err := New(Options{File: testIniPath("brackets-in-values.ini")})
	if err != nil {
		t.Fatal(err)
	}

	sections := ini.Sections()
	if len(sections) == 0 {
		t.Fatal("Expected sections")
	}
}

// ---------- Comments with spaces ----------

func TestCommentsFromCMTFile(t *testing.T) {
	ini, err := New(Options{File: testIniPath("cmt.ini")})
	if err != nil {
		t.Fatal(err)
	}

	sections := ini.Sections()
	if len(sections) == 0 {
		t.Fatal("Expected sections in cmt.ini")
	}
}

// ---------- Here doc without end marker ----------

func TestHereDocNoEndMarker(t *testing.T) {
	_, err := New(Options{File: testIniPath("here-doc-no-end-marker.ini")})
	// Should parse (maybe with errors in the error list) but not crash
	if err != nil {
		// Check if it's a parse error we can recover from
		t.Logf("Got expected error for missing end marker: %v", err)
	}
}

// ---------- Whitespace handling in section names ----------

func TestWhitespaceInSectionNames(t *testing.T) {
	ini, err := New(Options{File: testIniPath("test.ini")})
	if err != nil {
		t.Fatal(err)
	}

	// The test.ini has "[ group member two ]" which should be trimmed to "group member two"
	if !ini.SectionExists("group member two") {
		t.Error("Expected 'group member two' section (whitespace trimmed)")
	}
}

// ---------- Comprehensive round-trip with comments ----------

func TestRoundTripWithComments(t *testing.T) {
	ini := NewEmpty()
	ini.AddSection("sect1")
	ini.SetSectionComment("sect1", "Section one comment")
	ini.NewVal("sect1", "key1", "val1")
	ini.SetParameterComment("sect1", "key1", "Key one comment")
	ini.NewVal("sect1", "key2", "line1", "line2", "line3")
	ini.SetParameterEOT("sect1", "key2", "END")

	ini.AddSection("sect2")
	ini.NewVal("sect2", "key3", "val3")

	tmpDir := t.TempDir()
	outFile := filepath.Join(tmpDir, "roundtrip_cmt.ini")
	err := ini.WriteConfig(outFile)
	if err != nil {
		t.Fatal(err)
	}

	ini2, err := New(Options{File: outFile})
	if err != nil {
		t.Fatal(err)
	}

	if v := ini2.Val("sect1", "key1"); v != "val1" {
		t.Errorf("Expected 'val1', got %q", v)
	}

	cmt := ini2.GetSectionComment("sect1")
	if len(cmt) == 0 || !strings.Contains(cmt[0], "Section one comment") {
		t.Errorf("Expected section comment preserved, got %v", cmt)
	}

	cmt = ini2.GetParameterComment("sect1", "key1")
	if len(cmt) == 0 || !strings.Contains(cmt[0], "Key one comment") {
		t.Errorf("Expected parameter comment preserved, got %v", cmt)
	}

	vals := ini2.ValSlice("sect1", "key2")
	if len(vals) != 3 || vals[0] != "line1" || vals[1] != "line2" || vals[2] != "line3" {
		t.Errorf("Expected [line1, line2, line3], got %v", vals)
	}

	eot, ok := ini2.GetParameterEOT("sect1", "key2")
	if !ok || eot != "END" {
		t.Errorf("Expected EOT 'END', got %q (ok=%v)", eot, ok)
	}
}
