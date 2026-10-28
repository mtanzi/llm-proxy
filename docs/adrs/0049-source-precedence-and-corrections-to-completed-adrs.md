# ADR 0049: Source precedence and corrections to Completed ADRs

Status: Completed

Date: 2026-10-26

## Context

The ADR README said "if an ADR and a document disagree, the document wins". That rule ignores landed code and Completed ADRs. After the first ADRs landed, the documents kept sketches that the code had replaced, and three Completed ADRs hold statements that the code or later decisions contradict.

This ADR amends Completed ADRs 0001, 0003 and 0004. None is edited.

## Decision

One precedence rule replaces the README rule. When sources disagree, the first source in this list wins:

1. Landed code.
2. Completed ADRs.
3. The architecture documents, for work not yet built.
4. ADRs not yet built (Accepted or Proposed).

The order ranks sources as descriptions of current behaviour. A source lower in the list that disagrees with a higher one gets a fix. One exception: an Accepted ADR that amends landed code (for example ADR 0047 and ADR 0048) records the planned target. Its decision wins over the code until its Work required lands; after that, code and ADR agree. A Completed ADR is never edited; a change to one is a new ADR that names it. The design document (`llm-proxy-design.md`) holds only the requirements, the firstmate integration and the sources.

Corrections to Completed ADRs. Each states what the code does today.

- **ADR 0001, package list.** `internal/` holds 11 packages, not 10: `core`, `family/anthropic`, `proxy`, `httpmw`, `keyring`, `recorder`, `api`, `config`, `store/postgres`, `store/storetest` and `ui`. `httpmw` holds the request-lifecycle steps both listeners share: recover, request id, access log with header redaction, and the request-size limit.
- **ADR 0003, `SessionKey` reference.** The `SessionKey` derivation listed in the pure rules of `core` is removed by ADR 0048. Session handling is now in ADR 0010 (the header only, no derivation). ADR 0003 still describes `core.ID` and `core.MicroUSD` correctly; documents that sketch `uuid.UUID` or `decimal.Decimal` in `core` are wrong.
- **ADR 0004, "every key can be overridden".** Only scalar keys have an environment override named `LLM_PROXY_<SECTION>_<KEY>`. The lists (`providers`, `keys`, `pricing`) are not overridable. A secret reference is overridden by its parts (`LLM_PROXY_DATABASE_URL_ENV` or `LLM_PROXY_DATABASE_URL_FILE`). deployment.md already says this.
- **ADR 0004 and ADR 0008, credential modes.** See ADR 0047.

## Consequences

- A reader who finds a conflict knows which source to trust, and which to fix.
- The ADR README keeps the Completed ADRs untouched as a record, and this ADR is the place where their corrections live.
- References to design document sections 3 to 9 in Completed ADRs (0002, 0007) no longer resolve. The architecture documents hold that content: sections 3.4 and 4.3 are in backend.md "Extractor" and "Data model", and sections 1.2 and 7.2 are in the architecture README and backend.md "Shape".

## Work required

- None in code for the precedence rule and the package list.
- Code that follows from the amendments is listed in ADR 0047 and ADR 0048.

## Depends on

- ADR 0001, ADR 0003, ADR 0004.

## References

- adrs/README.md, "Status meanings".
- architecture/README.md, "Source precedence".
