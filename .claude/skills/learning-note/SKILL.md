---
name: learning-note
description: "Crear una nota de aprendizaje (learning note) en dev-notes/ y registrarla como anexo en el documento vivo dev-notes/README.md. Usar cuando Gonzalo pida guardar/documentar una explicación (memoria, tipos, concurrencia, GC, etc.) como documento, mencione 'nota', 'anexo', 'documento de esta explicación' o 'añádelo al readme'."
---

# Learning note

Convert an explanation given in conversation into a permanent learning note inside
`dev-notes/`, and register it in the living document `dev-notes/README.md`.

## Steps

1. **Write the note** at `dev-notes/<kebab-case-topic>.md`, in English, with this shape:
   - Title + one-line context: why this matters for grizzly (which design decision it
     supports).
   - The detailed explanation, restructured for reading (not a chat transcript).
   - A **"The simple version"** section: the plain analogy/ELI5 retelling.
   - A **"Further reading"** section with 1-2 links. **Verify each URL with WebFetch
     before citing it** — never include unverified links. Add one sentence per link
     explaining what it covers and why it's a good next step.

2. **Register it in `dev-notes/README.md`**:
   - Add a bullet under **Annexes (learning notes)**: `[Title](file.md) — one-line
     summary` mentioning which decision it underpins.
   - If the explanation resolved an open question or produced a decision, move/update
     the relevant entries in **Core design decisions** / **Open questions** too.

3. **Report back** with the file path and what changed in the README.

## Conventions

- English for all doc content; conversation with Gonzalo stays in Spanish.
- kebab-case filenames.
- Don't duplicate content between the note and README — the README links, the note
  explains.
- These are learning notes, not design guides: design guides are written by Gonzalo
  himself. A note explains a concept; it may *record* a decision, not *make* one.
