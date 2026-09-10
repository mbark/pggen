package clickhouse_multi

import "fmt"

// RecordType is what --go-type maps the enum column to, in place of the string
// chgen would pick on its own.
//
// It implements sql.Scanner because clickhouse-go decodes into the Go type the
// column maps to natively, or into a sql.Scanner — and into nothing else. A
// plain named type like `type RecordType string` compiles and then fails at
// run time with "converting Enum8 to *RecordType is unsupported", so an
// override that is only a rename does not work here.
type RecordType struct {
	Label string
}

func (r *RecordType) Scan(src any) error {
	s, ok := src.(string)
	if !ok {
		return fmt.Errorf("scan RecordType: want string, got %T", src)
	}
	r.Label = s
	return nil
}

func (r RecordType) String() string { return r.Label }
