package memory

import (
	"encoding/json"
	"fmt"
	"strconv"
)

// Go's JSON decoder replaces unpaired UTF-16 escapes with U+FFFD. Canonical
// state must reject that lossy decoding while accepting ordinary JSON escapes.
// JSON syntax is checked by the standard library; this scan only checks scalars.
func validateJSONUnicode(data []byte) error {
	if !json.Valid(data) {
		return fmt.Errorf("invalid structured JSON")
	}
	inString := false
	for i := 0; i < len(data); i++ {
		switch data[i] {
		case '"':
			inString = !inString
		case '\\':
			if !inString {
				continue
			}
			i++
			if data[i] != 'u' {
				continue
			}
			// json.Valid guarantees that every Unicode escape has four hex digits.
			unit, _ := strconv.ParseUint(string(data[i+1:i+5]), 16, 16)
			i += 4
			if unit >= 0xd800 && unit <= 0xdbff {
				if i+6 >= len(data) || data[i+1] != '\\' || data[i+2] != 'u' {
					return fmt.Errorf("unpaired JSON Unicode surrogate")
				}
				low, err := strconv.ParseUint(string(data[i+3:i+7]), 16, 16)
				if err != nil || low < 0xdc00 || low > 0xdfff {
					return fmt.Errorf("unpaired JSON Unicode surrogate")
				}
				i += 6
			} else if unit >= 0xdc00 && unit <= 0xdfff {
				return fmt.Errorf("unpaired JSON Unicode surrogate")
			}
		}
	}
	return nil
}
