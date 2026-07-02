// Package types holds protokit's generic, backend-neutral type utilities:
// classifying a proto field to a neutral schema.FieldType (ClassifyField),
// deciding whether a message relationalizes into its own table
// (Relationalizable), and the datasource Provider a generator groups by. Backend
// type projections (SQL, Solidity) live in each generator, not here.
package types

import "fmt"

// Provider identifies the datasource backend a generator groups by. protokit
// stores it as a plain string on schema.Database and uses it only for grouping
// and conflict detection; each generator interprets the value itself.
type Provider string

const (
	Postgres Provider = "postgres"
	MongoDB  Provider = "mongodb"
	// EVM marks a datasource whose backend is an EVM chain (Solidity contracts +
	// an off-chain indexer) rather than a SQL database. The IR still infers
	// canonical PostgreSQL types from the proto; the solidity and subgraph targets
	// project those onto chain-native types. Selecting EVM gates a datasource to
	// the chain targets and away from the relational ones.
	EVM Provider = "evm"
)

// ParseProvider normalizes a datasource provider string. Empty means Postgres.
func ParseProvider(s string) (Provider, error) {
	switch s {
	case "", "postgres", "postgresql":
		return Postgres, nil
	case "mongodb", "mongo":
		return MongoDB, nil
	case "evm", "ethereum":
		return EVM, nil
	default:
		return "", fmt.Errorf("unknown datasource provider %q (want postgres, mongodb, or evm)", s)
	}
}
