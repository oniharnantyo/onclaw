# Vendored anti-slop oxlint plugin

Source: [dmmulroy/anti-slop](https://github.com/dmmulroy/anti-slop), installed
2026-10-13 via the `install-anti-slop` skill (`.zcode/skills/install-anti-slop/`,
registered in `skills-lock.json`, skill hash `4031728fbe75bdcad6ee3208fd52b5d66e167b056fefee1fa9758e9a6cb9c0c8`).

**Exact upstream commit: unknown.** The skill ships a bundled pristine snapshot
of the plugin; the copy was verified against upstream HEAD at install time
(`c44ef22ca116d0ba62a3ff663a0bd13a3f3fa40b`) and does **not** match it
content-wise (HEAD reformats the sources with tab indentation), so the snapshot
predates that revision and no recoverable commit could be established.

## Installed layout

- `index.ts` — plugin entry, registers 18 generic rules.
- `rules/*.ts` — one file per rule; `shared/*.ts` — rule helpers.
- `effect/` — opt-in Effect-TS plugin; **not registered** in this repository
  (no `effect` manifest dependency).
- `vendor/eslint-stylistic/` — vendored `padding-line-between-statements`
  backing `require-readable-spacing`; see its own `UPSTREAM.md` and keep its
  `LICENSE` with every redistribution.

## Registration (vite.config.ts)

- `lint.jsPlugins`: `{ name: "anti-slop", specifier: "./tools/oxlint/anti-slop/index.ts" }`.
- All 18 `anti-slop/*` rules enabled at `"error"`, plus native companion
  `oxc/no-accumulating-spread` at `"error"` (pairs with
  `anti-slop/no-reduce-accumulator-copy`).
- `lint.ignorePatterns` and `fmt.ignorePatterns` exclude this directory and the
  repository's agent directories so `vp check` neither lints nor reformats the
  vendored sources or installed agent assets.

## Intentional deviations from the skill's default procedure

- No standalone `oxlint` dependency was installed: vite-plus (`vp`) bundles and
  runs oxlint 1.85.0 in this repository. `@oxlint/plugins` is pinned to exactly
  `1.85.0` to match the bundled engine, so both move together on future
  toolchain upgrades.
- The Effect plugin is not registered (see above).

## Status at install time

The full `vp check` initially reported 493 findings on owned source (307
`require-readable-spacing`, 94 `require-safety-comment-for-type-assertion`,
29 `no-unsafe-dictionary-type`, 19 `no-chained-type-assertions`,
17 `no-runtime-typeof`, 13 `no-unknown-parameters`, 11
`no-known-value-widening`, 3 `no-unknown-returns`).

**Cleaned up the same day (2026-10-13, user-authorized).** The 307 spacing
findings went through `vp lint --fix`; the rest were fixed properly — named
contracts introduced (`ListEnvelopeBody`, `FetcherError` + `EnvelopeValue`/
`EnvelopeField`, `LogValue`/`LogObject` in the logger, `ClaimBag`/`TokenBag`
extends patterns in auth types, `CreateValueBody`/`EditValueBody`/
`ToggleValueBody`, named page-copy return types), a next-auth `Session`
module augmentation (`src/types/next-auth.d.ts`: `user: SessionUser`,
`error?: string`) that removed both session casts, zod parsing of the backend
error `fields` in settings `values.ts`, `Object.assign(globalThis, …)` and an
exported `axiosInstance` in tests instead of double casts, and `// SAFETY:`
justifications on the handful of remaining boundary casts (the axios shim in
the fetcher, the snackbar style override, deliberately malformed test
fixtures). Final state: full unscoped `vp check` green, 171/171 tests green,
second fix/format pass stable.
