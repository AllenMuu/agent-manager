package memory

import "unicode/utf8"

// ValidateNewRecord lets external providers enforce the canonical input contract.
func ValidateNewRecord(input NewRecord) error { return input.validate() }

// ValidateRecord validates a complete provider record against its exact owner.
func ValidateRecord(record Record, owner Owner) error { return validateGatewayRecord(record, owner) }

// ValidateCanonicalJSON rejects lossy UTF-8/UTF-16 decoding before unmarshalling.
func ValidateCanonicalJSON(data []byte) error {
	if !utf8.Valid(data) {
		return ErrInvalidInput
	}
	if validateJSONUnicode(data) != nil {
		return ErrInvalidInput
	}
	return nil
}

// ValidateOwner enforces exact neutral owner partition identifiers.
func ValidateOwner(owner Owner) error { return owner.validate() }

// ValidateRecordID rejects identifiers that cannot survive canonical encoding.
func ValidateRecordID(id RecordID) error {
	if validateStructuredToken("record ID", string(id)) != nil {
		return ErrInvalidInput
	}
	return nil
}
