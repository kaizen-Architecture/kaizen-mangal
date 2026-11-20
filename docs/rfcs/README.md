# 📜 kaizen-mangal RFCs (Requests for Comments)

This directory contains the architecture proposals and technical specifications for **kaizen-mangal**, the scraping and download core of the Kaizen ecosystem.

RFCs follow the ecosystem-wide process defined in [Kaizen RFC 0000 — RFC Process & Architectural Governance](https://github.com/kaizen-Architecture/Kaizen/blob/main/docs/rfcs/0000-rfc-process.md):

1. **Proposal:** add `docs/rfcs/NNNN-<slug>.md` with status `DRAFT` and open a Pull Request for review.
2. **Review:** comments happen inline on the Pull Request. Open questions are resolved in the document itself.
3. **Approval:** status moves to `APPROVED`; a tracking issue is opened with the implementation phases as checklists.
4. **Implementation:** `feat/<slug>` / `fix/<slug>` branches, Conventional Commits, PR footer `Closes #<issue>` + `Ref: docs/rfcs/NNNN-<slug>.md`.
5. **Release:** staging pre-release validation, then merge to `main` and release-please tags the version.

## 📋 RFC Index

| ID | Title | Status | Author | Date |
|---|---|---|---|---|
| [RFC 0001](./0001-network-resilience-and-modernization.md) | Network Resilience, Download Integrity, Headless Fix & Toolchain Modernization | ✅ APPROVED | d4nj3s | 2026-11-19 |
