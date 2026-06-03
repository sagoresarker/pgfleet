package timescale

import (
	"strings"

	"github.com/sagoresarker/pgfleet/internal/apperr"
)

// The SQL builders below return the exact DDL/statement to execute. Each
// validates every interpolated name (the injection boundary in validate.go)
// before building the string. Object names are passed to TimescaleDB management
// functions as single-quoted string literals; because validation guarantees the
// names contain only [a-zA-Z0-9_.], they cannot escape the literal.

// CreateHypertableSQL builds a statement converting table into a hypertable
// partitioned by range on timeColumn, using the modern by_range() dimension
// builder.
func CreateHypertableSQL(table, timeColumn string) (string, error) {
	if err := ValidateQualifiedName(table); err != nil {
		return "", err
	}
	if err := ValidateIdent(timeColumn); err != nil {
		return "", err
	}
	return "SELECT create_hypertable('" + table + "', by_range('" + timeColumn + "'))", nil
}

// AddRetentionPolicySQL builds a statement adding a data-retention policy that
// drops chunks older than dropAfter (an interval string, e.g. "30 days").
func AddRetentionPolicySQL(hypertable, dropAfter string) (string, error) {
	if err := ValidateQualifiedName(hypertable); err != nil {
		return "", err
	}
	if err := ValidateInterval(dropAfter); err != nil {
		return "", err
	}
	return "SELECT add_retention_policy('" + hypertable + "', INTERVAL '" + dropAfter + "')", nil
}

// RemoveRetentionPolicySQL builds a statement removing the retention policy from
// hypertable.
func RemoveRetentionPolicySQL(hypertable string) (string, error) {
	if err := ValidateQualifiedName(hypertable); err != nil {
		return "", err
	}
	return "SELECT remove_retention_policy('" + hypertable + "')", nil
}

// EnableCompressionSQL builds an ALTER TABLE enabling columnar compression on
// hypertable. segmentBy and orderBy are optional comma-separated identifier
// lists; an empty value omits that clause.
func EnableCompressionSQL(hypertable, segmentBy, orderBy string) (string, error) {
	if err := ValidateQualifiedName(hypertable); err != nil {
		return "", err
	}
	clauses := []string{"timescaledb.compress"}
	if segmentBy != "" {
		if err := ValidateIdentList(segmentBy); err != nil {
			return "", err
		}
		clauses = append(clauses, "timescaledb.compress_segmentby='"+segmentBy+"'")
	}
	if orderBy != "" {
		if err := ValidateIdentList(orderBy); err != nil {
			return "", err
		}
		clauses = append(clauses, "timescaledb.compress_orderby='"+orderBy+"'")
	}
	return "ALTER TABLE " + hypertable + " SET (" + strings.Join(clauses, ", ") + ")", nil
}

// AddCompressionPolicySQL builds a statement adding a policy that compresses
// chunks older than compressAfter.
func AddCompressionPolicySQL(hypertable, compressAfter string) (string, error) {
	if err := ValidateQualifiedName(hypertable); err != nil {
		return "", err
	}
	if err := ValidateInterval(compressAfter); err != nil {
		return "", err
	}
	return "SELECT add_compression_policy('" + hypertable + "', INTERVAL '" + compressAfter + "')", nil
}

// RemoveCompressionPolicySQL builds a statement removing the compression policy
// from hypertable.
func RemoveCompressionPolicySQL(hypertable string) (string, error) {
	if err := ValidateQualifiedName(hypertable); err != nil {
		return "", err
	}
	return "SELECT remove_compression_policy('" + hypertable + "')", nil
}

// CreateContinuousAggregateSQL builds a CREATE MATERIALIZED VIEW for a
// continuous aggregate named name, defined by query.
//
// query is caller-supplied SQL. It is trusted operator input gated by RBAC at
// the API layer, but as a defense in depth this builder still rejects a query
// containing a semicolon to prevent statement chaining (the view body is
// concatenated into a single CREATE statement).
func CreateContinuousAggregateSQL(name, query string) (string, error) {
	if err := ValidateIdent(name); err != nil {
		return "", err
	}
	q := strings.TrimSpace(query)
	if q == "" {
		return "", apperr.New(apperr.KindInvalid, "timescale: empty continuous aggregate query")
	}
	if strings.Contains(query, ";") {
		return "", apperr.New(apperr.KindInvalid, "timescale: continuous aggregate query must not contain ';'")
	}
	return "CREATE MATERIALIZED VIEW " + name + " WITH (timescaledb.continuous) AS " + q + " WITH NO DATA", nil
}

// AddContinuousAggregatePolicySQL builds a statement adding a refresh policy to
// the continuous aggregate name. startOffset/endOffset/scheduleInterval are
// interval strings.
func AddContinuousAggregatePolicySQL(name, startOffset, endOffset, scheduleInterval string) (string, error) {
	if err := ValidateIdent(name); err != nil {
		return "", err
	}
	if err := ValidateInterval(startOffset); err != nil {
		return "", err
	}
	if err := ValidateInterval(endOffset); err != nil {
		return "", err
	}
	if err := ValidateInterval(scheduleInterval); err != nil {
		return "", err
	}
	return "SELECT add_continuous_aggregate_policy('" + name + "', " +
		"start_offset => INTERVAL '" + startOffset + "', " +
		"end_offset => INTERVAL '" + endOffset + "', " +
		"schedule_interval => INTERVAL '" + scheduleInterval + "')", nil
}
