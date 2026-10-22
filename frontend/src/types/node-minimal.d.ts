// Minimal ambient typings for the node builtins that test files import:
// buddies' frontend does not ship @types/node, and vitest resolves these
// modules fine at runtime — only tsc needs the declarations. If @types/node
// is ever added, delete this file.
declare module "node:fs" {
  export function readFileSync(path: string, encoding: string): string;
}

declare module "node:path" {
  export function resolve(...segments: string[]): string;
}

interface ImportMeta {
  readonly dirname: string;
}
