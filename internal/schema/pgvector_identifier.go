package schema

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

var invalidIdentifierRunes = regexp.MustCompile(`[^a-z0-9_]+`)

var postgresReservedWords = map[string]struct{}{
	"all": {}, "analyze": {}, "and": {}, "any": {}, "array": {}, "as": {}, "asc": {}, "asymmetric": {},
	"both": {}, "case": {}, "cast": {}, "check": {}, "collate": {}, "column": {}, "constraint": {}, "create": {},
	"current_catalog": {}, "current_date": {}, "current_role": {}, "current_time": {}, "current_timestamp": {}, "current_user": {},
	"default": {}, "deferrable": {}, "desc": {}, "distinct": {}, "do": {}, "else": {}, "end": {}, "except": {}, "false": {},
	"fetch": {}, "for": {}, "foreign": {}, "from": {}, "grant": {}, "group": {}, "having": {}, "in": {}, "initially": {},
	"intersect": {}, "into": {}, "lateral": {}, "leading": {}, "limit": {}, "localtime": {}, "localtimestamp": {}, "not": {},
	"null": {}, "offset": {}, "on": {}, "only": {}, "or": {}, "order": {}, "placing": {}, "primary": {}, "references": {},
	"returning": {}, "select": {}, "session_user": {}, "some": {}, "symmetric": {}, "table": {}, "then": {}, "to": {},
	"trailing": {}, "true": {}, "union": {}, "unique": {}, "user": {}, "using": {}, "variadic": {}, "when": {}, "where": {},
	"window": {}, "with": {},
}

// SanitizePGIdentifier converts a source database identifier into an unquoted
// PostgreSQL-safe identifier for schema planning. It lowercases names, replaces
// unsupported characters with underscores, avoids leading digits, and appends a
// suffix for PostgreSQL reserved words.
func SanitizePGIdentifier(input string) (string, error) {
	identifier := strings.ToLower(strings.TrimSpace(input))
	identifier = invalidIdentifierRunes.ReplaceAllString(identifier, "_")
	identifier = strings.TrimRight(identifier, "_")
	parts := strings.FieldsFunc(identifier, func(r rune) bool { return r == '_' })
	if strings.HasPrefix(identifier, "_") && len(parts) > 0 {
		parts[0] = "_" + parts[0]
	}
	identifier = strings.Join(parts, "_")
	if identifier == "" {
		return "", fmt.Errorf("identifier %q does not contain PostgreSQL-safe characters", input)
	}
	first := rune(identifier[0])
	if unicode.IsDigit(first) {
		identifier = "t_" + identifier
	}
	if _, reserved := postgresReservedWords[identifier]; reserved {
		identifier += "_"
	}
	return identifier, nil
}

// UniquePGIdentifiers builds deterministic source-to-target identifier mappings
// and appends numeric suffixes when multiple source names sanitize to the same
// PostgreSQL identifier.
func UniquePGIdentifiers(inputs []string) (map[string]string, error) {
	result := make(map[string]string, len(inputs))
	used := make(map[string]int, len(inputs))
	for _, input := range inputs {
		base, err := SanitizePGIdentifier(input)
		if err != nil {
			return nil, err
		}
		count := used[base]
		used[base] = count + 1
		identifier := base
		if count > 0 {
			identifier = fmt.Sprintf("%s_%d", base, count+1)
		}
		result[input] = identifier
	}
	return result, nil
}
