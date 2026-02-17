// Package inifiles provides a Go port of Perl's Config::IniFiles module.
//
// It reads and writes INI-style configuration files, preserving comments,
// parameter order, and supporting features like multi-line values (HERE docs),
// continuation lines, section groups, default sections, case-insensitive mode,
// and configuration import/overlay.
package inifiles

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
)

// IniFile represents a parsed INI configuration file.
type IniFile struct {
	// Ordered list of section names
	sects []string
	// Section existence map
	sectExists map[string]struct{}

	// Values: values[section][param] = []string
	values map[string]map[string][]string
	// Parameter names per section (ordered)
	params map[string][]string

	// Section comments: sCMT[section] = []string
	sCMT map[string][]string
	// Parameter comments: pCMT[section][param] = []string
	pCMT map[string]map[string][]string
	// Trailing comments: peCMT[section][param] = string
	peCMT map[string]map[string]string
	// HERE doc EOT markers: eot[section][param] = string
	eot map[string]map[string]string

	// Group membership: groups[groupname] = []string (full section names)
	groups map[string][]string

	// Trailing file comments (after last section)
	trailingComments []string

	// Configuration options
	filename              string
	defaultSect           string
	fallbackSect          string
	nocase                bool
	allowContinue         bool
	allowEmpty            bool
	noMultiline           bool
	negativedeltas        bool
	handleTrailingComment bool
	phpCompat             bool
	commentChar           string
	allowedCommentChars   string
	writeMode             os.FileMode
	lineEnding            string

	// Import support
	imported *IniFile

	// Delta tracking: which sections/params were modified by user
	mysects map[string]struct{}
	myparms map[string]map[string]struct{}

	// Parse errors
	errors []string
}

// Options configures IniFile creation.
type Options struct {
	// File is the path to the INI file to read.
	File string
	// Reader provides an alternative io.Reader source instead of a file path.
	Reader io.Reader
	// Content provides INI content as a string.
	Content string

	// Default is the section name to use as a default when a parameter
	// is not found in the requested section.
	Default string
	// Fallback is the section name for parameters found outside any section.
	Fallback string

	// NoCase enables case-insensitive section and parameter names.
	NoCase bool
	// AllowContinue enables backslash line continuation.
	AllowContinue bool
	// AllowEmpty allows empty files (no error if file is empty).
	AllowEmpty bool
	// NoMultiline outputs multi-valued params as repeated lines instead of HERE docs.
	NoMultiline bool
	// NegativeDeltas enables parsing of deletion comments in delta files.
	NegativeDeltas bool
	// HandleTrailingComment enables parsing of inline comments after values.
	HandleTrailingComment bool
	// PHPCompat enables PHP parse_ini_file compatibility mode.
	PHPCompat bool

	// CommentChar sets the primary comment character (default "#").
	CommentChar string
	// AllowedCommentChars sets all allowed comment characters (default "#;").
	AllowedCommentChars string

	// Import is another IniFile to import/inherit settings from.
	Import *IniFile
}

// New creates a new IniFile from the given options.
// Returns the IniFile and any parse errors.
func New(opts Options) (*IniFile, error) {
	ini := &IniFile{
		sects:      make([]string, 0),
		sectExists: make(map[string]struct{}),
		values:     make(map[string]map[string][]string),
		params:     make(map[string][]string),
		sCMT:       make(map[string][]string),
		pCMT:       make(map[string]map[string][]string),
		peCMT:      make(map[string]map[string]string),
		eot:        make(map[string]map[string]string),
		groups:     make(map[string][]string),
		mysects:    make(map[string]struct{}),
		myparms:    make(map[string]map[string]struct{}),

		commentChar:         "#",
		allowedCommentChars: "#;",
		lineEnding:          "\n",
		writeMode:           0666,

		defaultSect:           opts.Default,
		fallbackSect:          opts.Fallback,
		nocase:                opts.NoCase,
		allowContinue:         opts.AllowContinue,
		allowEmpty:            opts.AllowEmpty,
		noMultiline:           opts.NoMultiline,
		negativedeltas:        opts.NegativeDeltas,
		handleTrailingComment: opts.HandleTrailingComment,
		phpCompat:             opts.PHPCompat,
		filename:              opts.File,
	}

	if opts.CommentChar != "" {
		if len(opts.CommentChar) > 1 {
			return nil, fmt.Errorf("CommentChar must be a single character, got %q", opts.CommentChar)
		}
		ini.commentChar = opts.CommentChar
	}
	if opts.AllowedCommentChars != "" {
		ini.allowedCommentChars = opts.AllowedCommentChars
	}

	if opts.Import != nil {
		ini.imported = opts.Import
		ini.negativedeltas = true
		ini.importFrom(opts.Import)
	}

	// Determine the reader
	var reader io.Reader
	if opts.Content != "" {
		reader = strings.NewReader(opts.Content)
	} else if opts.Reader != nil {
		reader = opts.Reader
	} else if opts.File != "" {
		f, err := os.Open(opts.File)
		if err != nil {
			return nil, fmt.Errorf("failed to open file %q: %w", opts.File, err)
		}
		defer f.Close()

		// Capture file mode
		if fi, err := f.Stat(); err == nil {
			ini.writeMode = fi.Mode().Perm()
		}

		reader = f
	}

	if reader != nil {
		if err := ini.readConfig(reader); err != nil {
			return nil, err
		}
	} else if !opts.AllowEmpty {
		return nil, fmt.Errorf("no file, reader, or content provided")
	}

	return ini, nil
}

// NewEmpty creates a new empty IniFile.
func NewEmpty() *IniFile {
	ini, _ := New(Options{AllowEmpty: true})
	return ini
}

// Errors returns the list of parse errors from the last read.
func (ini *IniFile) Errors() []string {
	return ini.errors
}

// importFrom deep-copies sections and values from another IniFile.
func (ini *IniFile) importFrom(other *IniFile) {
	for _, sect := range other.sects {
		ini.ensureSection(sect)
		// Copy section comments
		if cmt, ok := other.sCMT[sect]; ok {
			ini.sCMT[sect] = append([]string{}, cmt...)
		}
		for _, param := range other.params[sect] {
			vals := other.values[sect][param]
			copied := append([]string{}, vals...)
			ini.values[sect][param] = copied
			ini.params[sect] = append(ini.params[sect], param)
			// Copy parameter comments
			if pcmt, ok := other.pCMT[sect]; ok {
				if cmt, ok := pcmt[param]; ok {
					if ini.pCMT[sect] == nil {
						ini.pCMT[sect] = make(map[string][]string)
					}
					ini.pCMT[sect][param] = append([]string{}, cmt...)
				}
			}
			// Copy trailing comments
			if pecmt, ok := other.peCMT[sect]; ok {
				if cmt, ok := pecmt[param]; ok {
					if ini.peCMT[sect] == nil {
						ini.peCMT[sect] = make(map[string]string)
					}
					ini.peCMT[sect][param] = cmt
				}
			}
			// Copy EOT markers
			if eots, ok := other.eot[sect]; ok {
				if e, ok := eots[param]; ok {
					if ini.eot[sect] == nil {
						ini.eot[sect] = make(map[string]string)
					}
					ini.eot[sect][param] = e
				}
			}
		}
	}
	// Copy groups
	for g, members := range other.groups {
		ini.groups[g] = append([]string{}, members...)
	}
}

// readConfig reads and parses an INI file from a reader.
func (ini *IniFile) readConfig(r io.Reader) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)

	var (
		currentSect     string
		pendingComments []string
		lineNum         int
		hasContent      bool
		allLines        []string
	)

	// Track params seen during this parse to distinguish "append multi-value"
	// from "first override of an imported value".
	seenParams := make(map[string]map[string]struct{})

	// Read all lines first to detect line endings
	for scanner.Scan() {
		allLines = append(allLines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading config: %w", err)
	}

	if len(allLines) == 0 && !ini.allowEmpty {
		return fmt.Errorf("file is empty")
	}

	// Strip UTF-8 BOM if present
	if len(allLines) > 0 && strings.HasPrefix(allLines[0], "\xef\xbb\xbf") {
		allLines[0] = strings.TrimPrefix(allLines[0], "\xef\xbb\xbf")
	}

	commentRegex := ini.buildCommentRegex()
	sectionRegex := regexp.MustCompile(`\A\s*\[\s*(.*\S)\s*\]\s*\z`)
	paramRegex := regexp.MustCompile(`^\s*([^=]*?[^=\s])\s*=\s*(.*)$`)
	hereDocRegex := regexp.MustCompile(`\A<<(.*)$`)
	continuationRegex := regexp.MustCompile(`\\\s*$`)

	// Negative delta regexes
	deletedSectRegex := regexp.MustCompile(`\A\s*[` + regexp.QuoteMeta(ini.allowedCommentChars) + `]\s*\[(\S|\S.*\S)\]\s+is\s+deleted\s*\z`)
	deletedParamRegex := regexp.MustCompile(`\A\s*[` + regexp.QuoteMeta(ini.allowedCommentChars) + `]\s*(\S.*\S|\S)\s+is\s+deleted\s*\z`)

	for lineNum = 0; lineNum < len(allLines); lineNum++ {
		line := allLines[lineNum]

		// Skip blank lines
		if strings.TrimSpace(line) == "" {
			continue
		}

		// Check for negative delta deletion markers
		if ini.negativedeltas {
			if m := deletedSectRegex.FindStringSubmatch(line); m != nil {
				sectName := m[1]
				if ini.nocase {
					sectName = strings.ToLower(sectName)
				}
				ini.removeSection(sectName)
				continue
			}
			if currentSect != "" {
				if m := deletedParamRegex.FindStringSubmatch(line); m != nil {
					paramName := m[1]
					if ini.nocase {
						paramName = strings.ToLower(paramName)
					}
					ini.removeParam(currentSect, paramName)
					continue
				}
			}
		}

		// Check for comments
		if commentRegex.MatchString(line) {
			pendingComments = append(pendingComments, line)
			continue
		}

		// Check for section header
		if m := sectionRegex.FindStringSubmatch(line); m != nil {
			hasContent = true
			sectName := m[1]
			if ini.nocase {
				sectName = strings.ToLower(sectName)
			}
			currentSect = sectName
			ini.ensureSection(currentSect)
			if len(pendingComments) > 0 {
				ini.sCMT[currentSect] = append([]string{}, pendingComments...)
				pendingComments = nil
			}
			ini.setGroupMember(currentSect)
			continue
		}

		// Check for parameter assignment
		if m := paramRegex.FindStringSubmatch(line); m != nil {
			hasContent = true
			paramName := m[1]
			rawValue := m[2]

			if currentSect == "" {
				if ini.fallbackSect != "" {
					currentSect = ini.fallbackSect
					ini.ensureSection(currentSect)
				} else {
					ini.errors = append(ini.errors, fmt.Sprintf("line %d: parameter %q found outside a section", lineNum+1, paramName))
					continue
				}
			}

			if ini.nocase {
				paramName = strings.ToLower(paramName)
			}

			// Handle trailing comments
			var trailingComment string
			if ini.handleTrailingComment {
				rawValue, trailingComment = ini.splitTrailingComment(rawValue)
			}

			// Check for HERE doc
			if hm := hereDocRegex.FindStringSubmatch(strings.TrimSpace(rawValue)); hm != nil {
				eotMarker := hm[1]
				var hereDocLines []string
				lineNum++
				foundEnd := false
				for lineNum < len(allLines) {
					hdLine := allLines[lineNum]
					if strings.TrimSpace(hdLine) == eotMarker {
						foundEnd = true
						break
					}
					hereDocLines = append(hereDocLines, hdLine)
					lineNum++
				}
				if !foundEnd {
					ini.errors = append(ini.errors, fmt.Sprintf("line %d: HERE doc for %q missing end marker %q", lineNum+1, paramName, eotMarker))
				}

				// Store HERE doc values
				ini.addParamValues(currentSect, paramName, hereDocLines, pendingComments, seenParams)
				// Store EOT marker
				if ini.eot[currentSect] == nil {
					ini.eot[currentSect] = make(map[string]string)
				}
				ini.eot[currentSect][paramName] = eotMarker
				pendingComments = nil
				if trailingComment != "" {
					if ini.peCMT[currentSect] == nil {
						ini.peCMT[currentSect] = make(map[string]string)
					}
					ini.peCMT[currentSect][paramName] = trailingComment
				}
				continue
			}

			// Handle continuation lines
			value := rawValue
			if ini.allowContinue {
				for continuationRegex.MatchString(value) && lineNum+1 < len(allLines) {
					value = continuationRegex.ReplaceAllString(value, "")
					lineNum++
					value += strings.TrimSpace(allLines[lineNum])
				}
			}

			// PHP compat: strip quotes
			if ini.phpCompat {
				value = stripPHPQuotes(value)
			}

			ini.addParamValues(currentSect, paramName, []string{value}, pendingComments, seenParams)
			pendingComments = nil
			if trailingComment != "" {
				if ini.peCMT[currentSect] == nil {
					ini.peCMT[currentSect] = make(map[string]string)
				}
				ini.peCMT[currentSect][paramName] = trailingComment
			}
			continue
		}

		// Unrecognized line
		ini.errors = append(ini.errors, fmt.Sprintf("line %d: unrecognized line: %q", lineNum+1, line))
	}

	// Trailing comments after last section
	if len(pendingComments) > 0 {
		ini.trailingComments = append(ini.trailingComments, pendingComments...)
	}

	if !hasContent && !ini.allowEmpty && ini.imported == nil {
		return fmt.Errorf("file is empty or has no valid content")
	}

	return nil
}

// buildCommentRegex returns a regex matching comment lines.
func (ini *IniFile) buildCommentRegex() *regexp.Regexp {
	escaped := regexp.QuoteMeta(ini.allowedCommentChars)
	return regexp.MustCompile(`\A\s*[` + escaped + `]`)
}

// splitTrailingComment separates a value from an inline trailing comment.
func (ini *IniFile) splitTrailingComment(rawValue string) (value, comment string) {
	// Look for comment char not inside quotes
	inSingle := false
	inDouble := false
	for i, ch := range rawValue {
		switch ch {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
		default:
			if !inSingle && !inDouble && strings.ContainsRune(ini.allowedCommentChars, ch) {
				// Check for preceding whitespace
				if i > 0 && rawValue[i-1] == ' ' {
					return strings.TrimRight(rawValue[:i], " \t"), string(rawValue[i:])
				}
			}
		}
	}
	return rawValue, ""
}

// stripPHPQuotes removes surrounding quotes from a value in PHP compat mode.
func stripPHPQuotes(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// ensureSection creates a section if it doesn't exist.
func (ini *IniFile) ensureSection(sect string) {
	if ini.nocase {
		sect = strings.ToLower(sect)
	}
	if _, ok := ini.sectExists[sect]; !ok {
		ini.sects = append(ini.sects, sect)
		ini.sectExists[sect] = struct{}{}
		ini.values[sect] = make(map[string][]string)
		ini.params[sect] = make([]string, 0)
	}
}

// addParamValues adds values for a parameter, appending if it already exists (multi-valued).
// seenParams tracks which params have been seen during the current parse; if a param was
// imported but is being seen for the first time in the overlay file, we replace instead of append.
func (ini *IniFile) addParamValues(sect, param string, vals []string, comments []string, seenParams map[string]map[string]struct{}) {
	if ini.nocase {
		sect = strings.ToLower(sect)
		param = strings.ToLower(param)
	}

	seen := false
	if seenParams != nil {
		if sp, ok := seenParams[sect]; ok {
			_, seen = sp[param]
		}
	}

	if _, exists := ini.values[sect][param]; !exists {
		ini.params[sect] = append(ini.params[sect], param)
		ini.values[sect][param] = make([]string, 0)
	} else if !seen {
		// First encounter in this parse for an imported param: replace values
		ini.values[sect][param] = make([]string, 0)
	}
	ini.values[sect][param] = append(ini.values[sect][param], vals...)

	// Mark as seen
	if seenParams != nil {
		if seenParams[sect] == nil {
			seenParams[sect] = make(map[string]struct{})
		}
		seenParams[sect][param] = struct{}{}
	}

	if len(comments) > 0 {
		if ini.pCMT[sect] == nil {
			ini.pCMT[sect] = make(map[string][]string)
		}
		ini.pCMT[sect][param] = append([]string{}, comments...)
	}
}

// removeSection removes a section and its data.
func (ini *IniFile) removeSection(sect string) {
	if ini.nocase {
		sect = strings.ToLower(sect)
	}
	if _, ok := ini.sectExists[sect]; !ok {
		return
	}
	newSects := make([]string, 0, len(ini.sects))
	for _, s := range ini.sects {
		if s != sect {
			newSects = append(newSects, s)
		}
	}
	ini.sects = newSects
	delete(ini.sectExists, sect)
	delete(ini.values, sect)
	delete(ini.params, sect)
	delete(ini.sCMT, sect)
	delete(ini.pCMT, sect)
	delete(ini.peCMT, sect)
	delete(ini.eot, sect)
	ini.removeGroupMember(sect)
}

// removeParam removes a parameter from a section.
func (ini *IniFile) removeParam(sect, param string) {
	if ini.nocase {
		sect = strings.ToLower(sect)
		param = strings.ToLower(param)
	}
	if _, ok := ini.sectExists[sect]; !ok {
		return
	}
	if _, ok := ini.values[sect][param]; !ok {
		return
	}
	delete(ini.values[sect], param)
	newParams := make([]string, 0)
	for _, p := range ini.params[sect] {
		if p != param {
			newParams = append(newParams, p)
		}
	}
	ini.params[sect] = newParams
	if pcmt, ok := ini.pCMT[sect]; ok {
		delete(pcmt, param)
	}
	if pecmt, ok := ini.peCMT[sect]; ok {
		delete(pecmt, param)
	}
	if e, ok := ini.eot[sect]; ok {
		delete(e, param)
	}
}

// setGroupMember registers a section as a group member if it matches "group member" pattern.
func (ini *IniFile) setGroupMember(sect string) {
	parts := strings.SplitN(sect, " ", 2)
	if len(parts) == 2 {
		group := parts[0]
		if ini.nocase {
			group = strings.ToLower(group)
		}
		ini.groups[group] = append(ini.groups[group], sect)
	}
}

// removeGroupMember removes a section from its group.
func (ini *IniFile) removeGroupMember(sect string) {
	parts := strings.SplitN(sect, " ", 2)
	if len(parts) == 2 {
		group := parts[0]
		if ini.nocase {
			group = strings.ToLower(group)
		}
		members := ini.groups[group]
		newMembers := make([]string, 0)
		for _, m := range members {
			if m != sect {
				newMembers = append(newMembers, m)
			}
		}
		if len(newMembers) == 0 {
			delete(ini.groups, group)
		} else {
			ini.groups[group] = newMembers
		}
	}
}

// ---------- Public API: Reading Values ----------

// Val returns the value of a parameter in a section.
// For multi-valued parameters, values are joined with "\n".
// If the parameter is not found in the section, the default section is checked.
// The optional defaultVal is returned if the parameter is not found anywhere.
func (ini *IniFile) Val(section, param string, defaultVal ...string) string {
	if ini.nocase {
		section = strings.ToLower(section)
		param = strings.ToLower(param)
	}
	if vals, ok := ini.values[section][param]; ok && len(vals) > 0 {
		return strings.Join(vals, "\n")
	}
	// Check default section
	if ini.defaultSect != "" {
		def := ini.defaultSect
		if ini.nocase {
			def = strings.ToLower(def)
		}
		if vals, ok := ini.values[def][param]; ok && len(vals) > 0 {
			return strings.Join(vals, "\n")
		}
	}
	if len(defaultVal) > 0 {
		return defaultVal[0]
	}
	return ""
}

// ValSlice returns all values of a multi-valued parameter as a slice.
func (ini *IniFile) ValSlice(section, param string) []string {
	if ini.nocase {
		section = strings.ToLower(section)
		param = strings.ToLower(param)
	}
	if vals, ok := ini.values[section][param]; ok {
		result := make([]string, len(vals))
		copy(result, vals)
		return result
	}
	// Check default section
	if ini.defaultSect != "" {
		def := ini.defaultSect
		if ini.nocase {
			def = strings.ToLower(def)
		}
		if vals, ok := ini.values[def][param]; ok {
			result := make([]string, len(vals))
			copy(result, vals)
			return result
		}
	}
	return nil
}

// Exists returns true if a parameter exists in the given section.
// Unlike Val, this does NOT check the default section.
func (ini *IniFile) Exists(section, param string) bool {
	if ini.nocase {
		section = strings.ToLower(section)
		param = strings.ToLower(param)
	}
	if svals, ok := ini.values[section]; ok {
		_, exists := svals[param]
		return exists
	}
	return false
}

// ---------- Public API: Section Management ----------

// Sections returns a list of all section names in order.
func (ini *IniFile) Sections() []string {
	result := make([]string, len(ini.sects))
	copy(result, ini.sects)
	return result
}

// SectionExists returns true if the section exists.
func (ini *IniFile) SectionExists(section string) bool {
	if ini.nocase {
		section = strings.ToLower(section)
	}
	_, ok := ini.sectExists[section]
	return ok
}

// AddSection creates a new section. Does nothing if it already exists.
func (ini *IniFile) AddSection(section string) {
	ini.ensureSection(section)
	ini.touchSection(section)
}

// DeleteSection removes a section and all its parameters.
func (ini *IniFile) DeleteSection(section string) bool {
	if ini.nocase {
		section = strings.ToLower(section)
	}
	if _, ok := ini.sectExists[section]; !ok {
		return false
	}
	ini.removeSection(section)
	ini.touchSection(section)
	return true
}

// RenameSection renames a section. If includeGroupMembers is true,
// group member sections are also renamed.
func (ini *IniFile) RenameSection(oldName, newName string, includeGroupMembers bool) bool {
	if ini.nocase {
		oldName = strings.ToLower(oldName)
		newName = strings.ToLower(newName)
	}
	if _, ok := ini.sectExists[oldName]; !ok {
		return false
	}
	if _, ok := ini.sectExists[newName]; ok {
		return false
	}

	ini.copySectionInternal(oldName, newName)
	ini.removeSection(oldName)

	if includeGroupMembers {
		oldParts := strings.SplitN(oldName, " ", 2)
		newParts := strings.SplitN(newName, " ", 2)
		if len(oldParts) >= 1 && len(newParts) >= 1 {
			oldGroup := oldParts[0]
			if members, ok := ini.groups[oldGroup]; ok {
				for _, m := range members {
					if m == oldName {
						continue
					}
					mParts := strings.SplitN(m, " ", 2)
					if len(mParts) == 2 {
						newMemberName := newParts[0] + " " + mParts[1]
						ini.copySectionInternal(m, newMemberName)
						ini.removeSection(m)
					}
				}
			}
		}
	}

	ini.touchSection(newName)
	return true
}

// CopySection copies a section to a new name. If includeGroupMembers is true,
// group member sections are also copied.
func (ini *IniFile) CopySection(oldName, newName string, includeGroupMembers bool) bool {
	if ini.nocase {
		oldName = strings.ToLower(oldName)
		newName = strings.ToLower(newName)
	}
	if _, ok := ini.sectExists[oldName]; !ok {
		return false
	}
	if _, ok := ini.sectExists[newName]; ok {
		return false
	}

	ini.copySectionInternal(oldName, newName)

	if includeGroupMembers {
		oldParts := strings.SplitN(oldName, " ", 2)
		newParts := strings.SplitN(newName, " ", 2)
		if len(oldParts) >= 1 && len(newParts) >= 1 {
			oldGroup := oldParts[0]
			if members, ok := ini.groups[oldGroup]; ok {
				for _, m := range members {
					mParts := strings.SplitN(m, " ", 2)
					if len(mParts) == 2 {
						newMemberName := newParts[0] + " " + mParts[1]
						if _, ok := ini.sectExists[newMemberName]; !ok {
							ini.copySectionInternal(m, newMemberName)
						}
					}
				}
			}
		}
	}

	ini.touchSection(newName)
	return true
}

// copySectionInternal copies all data from one section to another.
func (ini *IniFile) copySectionInternal(src, dst string) {
	ini.ensureSection(dst)

	// Copy params and values
	for _, param := range ini.params[src] {
		vals := ini.values[src][param]
		copied := make([]string, len(vals))
		copy(copied, vals)
		ini.values[dst][param] = copied
		ini.params[dst] = append(ini.params[dst], param)
	}

	// Copy comments
	if cmt, ok := ini.sCMT[src]; ok {
		ini.sCMT[dst] = append([]string{}, cmt...)
	}
	if pcmts, ok := ini.pCMT[src]; ok {
		ini.pCMT[dst] = make(map[string][]string)
		for k, v := range pcmts {
			ini.pCMT[dst][k] = append([]string{}, v...)
		}
	}
	if pecmts, ok := ini.peCMT[src]; ok {
		ini.peCMT[dst] = make(map[string]string)
		for k, v := range pecmts {
			ini.peCMT[dst][k] = v
		}
	}
	if eots, ok := ini.eot[src]; ok {
		ini.eot[dst] = make(map[string]string)
		for k, v := range eots {
			ini.eot[dst][k] = v
		}
	}

	ini.setGroupMember(dst)
}

// Parameters returns a list of parameter names in a section, in order.
func (ini *IniFile) Parameters(section string) []string {
	if ini.nocase {
		section = strings.ToLower(section)
	}
	if params, ok := ini.params[section]; ok {
		result := make([]string, len(params))
		copy(result, params)
		return result
	}
	return nil
}

// ---------- Public API: Modifying Values ----------

// NewVal creates or replaces a parameter with the given value(s).
// The section is created if it doesn't exist.
func (ini *IniFile) NewVal(section, param string, values ...string) {
	if ini.nocase {
		section = strings.ToLower(section)
		param = strings.ToLower(param)
	}

	ini.ensureSection(section)

	// Remove existing parameter if present
	if _, exists := ini.values[section][param]; exists {
		// Clear old values but keep in param list
		ini.values[section][param] = make([]string, 0)
	} else {
		ini.params[section] = append(ini.params[section], param)
	}

	ini.values[section][param] = append([]string{}, values...)
	ini.touchSection(section)
	ini.touchParameter(section, param)
}

// SetVal modifies an existing parameter value.
// Returns false if the parameter doesn't exist.
func (ini *IniFile) SetVal(section, param string, values ...string) bool {
	if ini.nocase {
		section = strings.ToLower(section)
		param = strings.ToLower(param)
	}

	if _, ok := ini.values[section]; !ok {
		return false
	}
	if _, ok := ini.values[section][param]; !ok {
		return false
	}

	ini.values[section][param] = append([]string{}, values...)
	ini.touchSection(section)
	ini.touchParameter(section, param)
	return true
}

// Push appends values to an existing multi-valued parameter.
// Returns false if the parameter doesn't exist.
func (ini *IniFile) Push(section, param string, values ...string) bool {
	if ini.nocase {
		section = strings.ToLower(section)
		param = strings.ToLower(param)
	}

	if _, ok := ini.values[section]; !ok {
		return false
	}
	if _, ok := ini.values[section][param]; !ok {
		return false
	}

	ini.values[section][param] = append(ini.values[section][param], values...)
	ini.touchSection(section)
	ini.touchParameter(section, param)
	return true
}

// DelVal deletes a parameter from a section.
// Returns false if the parameter doesn't exist.
func (ini *IniFile) DelVal(section, param string) bool {
	if ini.nocase {
		section = strings.ToLower(section)
		param = strings.ToLower(param)
	}

	if _, ok := ini.values[section]; !ok {
		return false
	}
	if _, ok := ini.values[section][param]; !ok {
		return false
	}

	ini.removeParam(section, param)
	ini.touchSection(section)
	ini.touchParameter(section, param)
	return true
}

// ---------- Public API: Comments ----------

// SetSectionComment sets the comment lines preceding a section.
// Lines are automatically prepended with the comment character if needed.
func (ini *IniFile) SetSectionComment(section string, comments ...string) {
	if ini.nocase {
		section = strings.ToLower(section)
	}
	formatted := ini.formatComments(comments)
	ini.sCMT[section] = formatted
}

// GetSectionComment returns the comment lines for a section.
func (ini *IniFile) GetSectionComment(section string) []string {
	if ini.nocase {
		section = strings.ToLower(section)
	}
	if cmt, ok := ini.sCMT[section]; ok {
		result := make([]string, len(cmt))
		copy(result, cmt)
		return result
	}
	return nil
}

// DeleteSectionComment removes the comment for a section.
func (ini *IniFile) DeleteSectionComment(section string) {
	if ini.nocase {
		section = strings.ToLower(section)
	}
	delete(ini.sCMT, section)
}

// SetParameterComment sets the comment lines preceding a parameter.
func (ini *IniFile) SetParameterComment(section, param string, comments ...string) {
	if ini.nocase {
		section = strings.ToLower(section)
		param = strings.ToLower(param)
	}
	if ini.pCMT[section] == nil {
		ini.pCMT[section] = make(map[string][]string)
	}
	formatted := ini.formatComments(comments)
	ini.pCMT[section][param] = formatted
}

// GetParameterComment returns the comment lines for a parameter.
func (ini *IniFile) GetParameterComment(section, param string) []string {
	if ini.nocase {
		section = strings.ToLower(section)
		param = strings.ToLower(param)
	}
	if pcmt, ok := ini.pCMT[section]; ok {
		if cmt, ok := pcmt[param]; ok {
			result := make([]string, len(cmt))
			copy(result, cmt)
			return result
		}
	}
	return nil
}

// DeleteParameterComment removes the comment for a parameter.
func (ini *IniFile) DeleteParameterComment(section, param string) {
	if ini.nocase {
		section = strings.ToLower(section)
		param = strings.ToLower(param)
	}
	if pcmt, ok := ini.pCMT[section]; ok {
		delete(pcmt, param)
	}
}

// SetParameterTrailingComment sets the inline comment after a parameter value.
func (ini *IniFile) SetParameterTrailingComment(section, param, comment string) bool {
	if ini.nocase {
		section = strings.ToLower(section)
		param = strings.ToLower(param)
	}
	if _, ok := ini.values[section]; !ok {
		return false
	}
	if _, ok := ini.values[section][param]; !ok {
		return false
	}
	if ini.peCMT[section] == nil {
		ini.peCMT[section] = make(map[string]string)
	}
	ini.peCMT[section][param] = comment
	return true
}

// GetParameterTrailingComment returns the trailing comment for a parameter.
// Returns ("", false) if the parameter doesn't exist.
func (ini *IniFile) GetParameterTrailingComment(section, param string) (string, bool) {
	if ini.nocase {
		section = strings.ToLower(section)
		param = strings.ToLower(param)
	}
	if _, ok := ini.values[section]; !ok {
		return "", false
	}
	if _, ok := ini.values[section][param]; !ok {
		return "", false
	}
	if pecmt, ok := ini.peCMT[section]; ok {
		if cmt, ok := pecmt[param]; ok {
			return cmt, true
		}
	}
	return "", true
}

// formatComments ensures each comment line starts with the comment character.
func (ini *IniFile) formatComments(lines []string) []string {
	commentRegex := ini.buildCommentRegex()
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		if !commentRegex.MatchString(line) {
			line = ini.commentChar + " " + line
		}
		result = append(result, line)
	}
	return result
}

// ---------- Public API: EOT Markers ----------

// GetParameterEOT returns the HERE doc end marker for a parameter.
// Returns ("", false) if the parameter doesn't use a HERE doc.
func (ini *IniFile) GetParameterEOT(section, param string) (string, bool) {
	if ini.nocase {
		section = strings.ToLower(section)
		param = strings.ToLower(param)
	}
	if eots, ok := ini.eot[section]; ok {
		if e, ok := eots[param]; ok {
			return e, true
		}
	}
	return "", false
}

// SetParameterEOT sets the HERE doc end marker for a parameter,
// forcing it to use HERE doc output style.
func (ini *IniFile) SetParameterEOT(section, param, marker string) {
	if ini.nocase {
		section = strings.ToLower(section)
		param = strings.ToLower(param)
	}
	if ini.eot[section] == nil {
		ini.eot[section] = make(map[string]string)
	}
	ini.eot[section][param] = marker
}

// DeleteParameterEOT removes the HERE doc marker for a parameter.
func (ini *IniFile) DeleteParameterEOT(section, param string) {
	if ini.nocase {
		section = strings.ToLower(section)
		param = strings.ToLower(param)
	}
	if eots, ok := ini.eot[section]; ok {
		delete(eots, param)
	}
}

// ---------- Public API: Groups ----------

// Groups returns a list of group names.
func (ini *IniFile) Groups() []string {
	result := make([]string, 0, len(ini.groups))
	for g := range ini.groups {
		result = append(result, g)
	}
	return result
}

// GroupMembers returns the full section names belonging to a group.
func (ini *IniFile) GroupMembers(group string) []string {
	if ini.nocase {
		group = strings.ToLower(group)
	}
	if members, ok := ini.groups[group]; ok {
		result := make([]string, len(members))
		copy(result, members)
		return result
	}
	return nil
}

// ---------- Public API: File Operations ----------

// SetFileName sets the filename for WriteConfig/RewriteConfig.
func (ini *IniFile) SetFileName(filename string) {
	ini.filename = filename
}

// GetFileName returns the current filename.
func (ini *IniFile) GetFileName() string {
	return ini.filename
}

// SetWriteMode sets the file permissions for writing.
func (ini *IniFile) SetWriteMode(mode os.FileMode) {
	ini.writeMode = mode
}

// GetWriteMode returns the current write mode.
func (ini *IniFile) GetWriteMode() os.FileMode {
	return ini.writeMode
}

// WriteConfig writes the configuration to a file.
func (ini *IniFile) WriteConfig(filename string) error {
	f, err := os.OpenFile(filename, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, ini.writeMode)
	if err != nil {
		return fmt.Errorf("failed to open %q for writing: %w", filename, err)
	}
	defer f.Close()

	return ini.OutputConfigToWriter(f, false)
}

// WriteConfigDelta writes only the changes relative to the imported config.
func (ini *IniFile) WriteConfigDelta(filename string) error {
	f, err := os.OpenFile(filename, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, ini.writeMode)
	if err != nil {
		return fmt.Errorf("failed to open %q for writing: %w", filename, err)
	}
	defer f.Close()

	return ini.OutputConfigToWriter(f, true)
}

// RewriteConfig rewrites the current file.
func (ini *IniFile) RewriteConfig() error {
	if ini.filename == "" {
		return fmt.Errorf("no filename set")
	}
	return ini.WriteConfig(ini.filename)
}

// OutputConfigToWriter writes the configuration to a writer.
func (ini *IniFile) OutputConfigToWriter(w io.Writer, delta bool) error {
	bw := bufio.NewWriter(w)
	defer bw.Flush()

	le := ini.lineEnding

	// Determine which sections to output
	sectsToOutput := ini.sects
	if delta && ini.imported != nil {
		// Only output sections that were modified
		sectsToOutput = make([]string, 0)
		for _, s := range ini.sects {
			if _, ok := ini.mysects[s]; ok {
				sectsToOutput = append(sectsToOutput, s)
			}
		}
		// Output deleted sections as comments
		for _, s := range ini.imported.sects {
			if _, ok := ini.sectExists[s]; !ok {
				fmt.Fprintf(bw, "%s [%s] is deleted%s", ini.commentChar, s, le)
			}
		}
	}

	for i, sect := range sectsToOutput {
		if i > 0 {
			fmt.Fprint(bw, le)
		}

		// Section comments
		if cmt, ok := ini.sCMT[sect]; ok {
			for _, c := range cmt {
				fmt.Fprintf(bw, "%s%s", c, le)
			}
		}

		// Skip section header for fallback section
		if sect != ini.fallbackSect {
			fmt.Fprintf(bw, "[%s]%s", sect, le)
		}

		// Parameters
		paramsToOutput := ini.params[sect]
		if delta && ini.imported != nil {
			paramsToOutput = make([]string, 0)
			if mp, ok := ini.myparms[sect]; ok {
				for _, p := range ini.params[sect] {
					if _, ok := mp[p]; ok {
						paramsToOutput = append(paramsToOutput, p)
					}
				}
			}
			// Output deleted params as comments
			if importedParams, ok := ini.imported.params[sect]; ok {
				for _, p := range importedParams {
					if _, exists := ini.values[sect][p]; !exists {
						fmt.Fprintf(bw, "%s %s is deleted%s", ini.commentChar, p, le)
					}
				}
			}
		}

		for _, param := range paramsToOutput {
			ini.outputParam(bw, sect, param, le)
		}
	}

	// Trailing comments
	for _, c := range ini.trailingComments {
		fmt.Fprintf(bw, "%s%s", c, le)
	}

	return bw.Flush()
}

// outputParam writes a single parameter to the writer.
func (ini *IniFile) outputParam(w *bufio.Writer, sect, param, le string) {
	// Parameter comments
	if pcmt, ok := ini.pCMT[sect]; ok {
		if cmt, ok := pcmt[param]; ok {
			for _, c := range cmt {
				fmt.Fprintf(w, "%s%s", c, le)
			}
		}
	}

	vals := ini.values[sect][param]

	// Trailing comment
	var trailingCmt string
	if pecmt, ok := ini.peCMT[sect]; ok {
		if cmt, ok := pecmt[param]; ok {
			trailingCmt = " " + cmt
		}
	}

	if len(vals) <= 1 {
		// Single value or empty
		val := ""
		if len(vals) == 1 {
			val = vals[0]
		}

		// Check for forced EOT
		if eots, ok := ini.eot[sect]; ok {
			if eotMark, ok := eots[param]; ok {
				fmt.Fprintf(w, "%s= <<%s%s%s", param, eotMark, trailingCmt, le)
				fmt.Fprintf(w, "%s%s", val, le)
				fmt.Fprintf(w, "%s%s", eotMark, le)
				return
			}
		}

		fmt.Fprintf(w, "%s=%s%s%s", param, val, trailingCmt, le)
	} else {
		// Multi-valued
		eotMark := ""
		if eots, ok := ini.eot[sect]; ok {
			if e, ok := eots[param]; ok {
				eotMark = e
			}
		}

		if ini.noMultiline || eotMark == "" {
			// Output as repeated parameters
			for _, v := range vals {
				fmt.Fprintf(w, "%s=%s%s", param, v, le)
			}
		} else {
			// Output as HERE doc
			if eotMark == "" {
				eotMark = ini.calcEOTMark(vals)
			}
			fmt.Fprintf(w, "%s= <<%s%s%s", param, eotMark, trailingCmt, le)
			for _, v := range vals {
				fmt.Fprintf(w, "%s%s", v, le)
			}
			fmt.Fprintf(w, "%s%s", eotMark, le)
		}
	}
}

// calcEOTMark generates a unique EOT marker that doesn't appear in the values.
func (ini *IniFile) calcEOTMark(vals []string) string {
	mark := "EOT"
	for {
		found := false
		for _, v := range vals {
			if strings.TrimSpace(v) == mark {
				found = true
				break
			}
		}
		if !found {
			return mark
		}
		mark = mark + "EOT"
	}
}

// String returns the configuration as a string.
func (ini *IniFile) String() string {
	var sb strings.Builder
	ini.OutputConfigToWriter(&sb, false)
	return sb.String()
}

// ---------- Public API: Misc ----------

// Delete clears all configuration data.
func (ini *IniFile) Delete() {
	ini.sects = make([]string, 0)
	ini.sectExists = make(map[string]struct{})
	ini.values = make(map[string]map[string][]string)
	ini.params = make(map[string][]string)
	ini.sCMT = make(map[string][]string)
	ini.pCMT = make(map[string]map[string][]string)
	ini.peCMT = make(map[string]map[string]string)
	ini.eot = make(map[string]map[string]string)
	ini.groups = make(map[string][]string)
	ini.trailingComments = nil
	ini.mysects = make(map[string]struct{})
	ini.myparms = make(map[string]map[string]struct{})
}

// ReadConfig re-reads the configuration file.
func (ini *IniFile) ReadConfig() error {
	if ini.filename == "" {
		return fmt.Errorf("no filename set")
	}

	// Clear existing data but keep options
	ini.sects = make([]string, 0)
	ini.sectExists = make(map[string]struct{})
	ini.values = make(map[string]map[string][]string)
	ini.params = make(map[string][]string)
	ini.sCMT = make(map[string][]string)
	ini.pCMT = make(map[string]map[string][]string)
	ini.peCMT = make(map[string]map[string]string)
	ini.eot = make(map[string]map[string]string)
	ini.groups = make(map[string][]string)
	ini.trailingComments = nil
	ini.errors = nil

	if ini.imported != nil {
		ini.importFrom(ini.imported)
	}

	f, err := os.Open(ini.filename)
	if err != nil {
		return fmt.Errorf("failed to open file %q: %w", ini.filename, err)
	}
	defer f.Close()

	return ini.readConfig(f)
}

// touchSection marks a section as user-modified (for delta support).
func (ini *IniFile) touchSection(section string) {
	if ini.nocase {
		section = strings.ToLower(section)
	}
	ini.mysects[section] = struct{}{}
}

// touchParameter marks a parameter as user-modified (for delta support).
func (ini *IniFile) touchParameter(section, param string) {
	if ini.nocase {
		section = strings.ToLower(section)
		param = strings.ToLower(param)
	}
	if ini.myparms[section] == nil {
		ini.myparms[section] = make(map[string]struct{})
	}
	ini.myparms[section][param] = struct{}{}
}
