/// <reference types="vite/client" />

interface ImportMetaEnv {
  // Set by `make desktop-fake`: run the app against made-up state instead of
  // the daemon, for looking at the design (#36).
  readonly VITE_FAKE?: string;
}

interface ImportMeta {
  readonly env: ImportMetaEnv;
}
