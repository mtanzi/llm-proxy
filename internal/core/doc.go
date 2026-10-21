// Package core holds the domain entities, pure rules and storage port interfaces. It imports the standard library only.
//
// The v1 storage model (ADR 0002) has three rules that every change to this package keeps:
//
//   - No bodies. No type, field or port method carries prompt, response, tool argument, tool result or provider error text. Tool calls are records of name, ids, sizes, duration and success or error.
//   - Every v1 write is an insert. The database receives inserts only, through the CallWriter port. There are no updates and no deletes except retention.
//   - Every relational view is derived in CallReader. Tool-call pairing, sessions, agent trees and statistics are computed when read, not maintained when written.
package core
