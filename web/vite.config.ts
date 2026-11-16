import { defineConfig } from 'vitest/config';
import react from '@vitejs/plugin-react';
import { fileURLToPath } from 'node:url';

export default defineConfig(({ mode }) => ({
  resolve: {
    alias: {
      '#console-profile': fileURLToPath(
        new URL(
          `./src/profiles/${mode === 'pages' ? 'public' : mode === 'fixtures' ? 'fixtures' : 'installed'}.ts`,
          import.meta.url,
        ),
      ),
    },
  },
  base: mode === 'pages' ? `${process.env.SITE_BASE || '/orbitjob'}/demo/` : '/console/',
  plugins: [
    react(),
    ...(mode !== 'pages' && mode !== 'fixtures'
      ? ([
          {
            name: 'installed-profile-boundary',
            generateBundle(_options, bundle) {
              for (const output of Object.values(bundle)) {
                if (output.type !== 'chunk') continue;
                for (const module of Object.keys(output.modules)) {
                  if (/\/src\/(api\/[^/]*demo|profiles\/(public|fixtures))\.tsx?$/.test(module))
                    this.error(`Installed Console must not contain demo modules: ${module}`);
                }
              }
            },
          },
        ] satisfies import('vite').Plugin[])
      : []),
    ...(mode === 'pages'
      ? [
          {
            name: 'public-demo-policy',
            transformIndexHtml() {
              return [
                {
                  tag: 'meta',
                  attrs: {
                    'http-equiv': 'Content-Security-Policy',
                    content:
                      "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; font-src 'self'; connect-src 'none'; object-src 'none'; base-uri 'none'; form-action 'none'",
                  },
                  injectTo: 'head-prepend' as const,
                },
              ];
            },
          },
        ]
      : []),
  ],
  build: {
    rolldownOptions: {
      output: {
        codeSplitting: {
          groups: [
            { name: 'react', test: /node_modules\/(react|react-dom|scheduler)\// },
            { name: 'validation', test: /node_modules\/zod\// },
          ],
        },
      },
    },
  },
  server: {
    watch: {
      ignored: [
        '**/test-results*/**',
        '**/installed-test-results/**',
        '**/playwright-report/**',
        '**/.runtime/**',
      ],
    },
    proxy: Object.fromEntries(
      ['/api', '/openapi.json', '/healthz'].map((path) => [
        path,
        { target: 'http://127.0.0.1:18080', changeOrigin: false },
      ]),
    ),
  },
  test: { include: ['src/**/*.test.ts'] },
}));
