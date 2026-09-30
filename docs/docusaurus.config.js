// @ts-check
// `@type` JSDoc annotations allow editor autocompletion and type checking
// (when paired with `@ts-check`).
// There are various equivalent ways to declare your Docusaurus config.
// See: https://docusaurus.io/docs/api/docusaurus-config

import {themes as prismThemes} from 'prism-react-renderer';

// This runs in Node.js - Don't use client-side code here (browser APIs, JSX...)

// The site is published to GitHub Pages as a project site (https://damaged0ne.github.io/shards/).
// Override with DOCS_URL / DOCS_BASE_URL when hosting it elsewhere (e.g. on a custom domain with DOCS_BASE_URL=/).
const url = process.env.DOCS_URL || 'https://damaged0ne.github.io';
const baseUrl = process.env.DOCS_BASE_URL || '/shards/';

// Markdown links and images are resolved against baseUrl by Docusaurus, but raw HTML/JSX
// attributes such as <img src="/img/..."> are not. This remark plugin prefixes site-absolute
// src/href attributes of JSX elements with baseUrl.
function remarkBaseUrl() {
  const prefix = baseUrl.replace(/\/$/, '');
  const walk = (node) => {
    if ((node.type === 'mdxJsxFlowElement' || node.type === 'mdxJsxTextElement') && Array.isArray(node.attributes)) {
      for (const attr of node.attributes) {
        if (
          attr.type === 'mdxJsxAttribute' &&
          (attr.name === 'src' || attr.name === 'href') &&
          typeof attr.value === 'string' &&
          attr.value.startsWith('/') &&
          !attr.value.startsWith('//') &&
          prefix &&
          !attr.value.startsWith(prefix + '/')
        ) {
          attr.value = prefix + attr.value;
        }
      }
    }
    if (Array.isArray(node.children)) {
      node.children.forEach(walk);
    }
  };
  return (tree) => walk(tree);
}

/** @type {import('@docusaurus/types').Config} */
const config = {
  title: 'shards',
  tagline: 'Self-hosted observability, alerting and incident center, operable by people and AI agents',
  favicon: 'img/icon.svg',
  url,
  baseUrl,
  organizationName: 'damaged0ne', // Usually your GitHub org/user name.
  projectName: 'shards', // Usually your repo name.
  onBrokenLinks: 'throw',
  onBrokenMarkdownLinks: 'throw',
  i18n: {
    defaultLocale: 'en',
    locales: ['en'],
  },

  presets: [
    [
      'classic',
      /** @type {import('@docusaurus/preset-classic').Options} */
      ({
        docs: {
          routeBasePath: '/',
          sidebarPath: './sidebars.js',
          editUrl:
            'https://github.com/damaged0ne/shards/tree/main/docs',
          remarkPlugins: [remarkBaseUrl],
        },
        theme: {
          customCss: './src/css/custom.css',
        },
      }),
    ],
  ],

  themeConfig:
    /** @type {import('@docusaurus/preset-classic').ThemeConfig} */
    ({
      navbar: {
        title: 'shards',
        logo: {
          alt: 'shards',
          src: 'img/icon.svg',
        },
        items: [
          {
            href: 'https://github.com/damaged0ne/shards',
            label: 'GitHub',
            position: 'right',
          },
        ],
      },
      footer: {
        style: 'dark',
        links: [
          {
            title: 'shards',
            items: [
              {
                label: 'Documentation',
                to: '/',
              },
              {
                label: 'Operator agents',
                to: '/agents/operator-agents',
              },
            ],
          },
          {
            title: 'Repositories',
            items: [
              {
                label: 'shards',
                href: 'https://github.com/damaged0ne/shards',
              },
              {
                label: 'shards-node-agent',
                href: 'https://github.com/damaged0ne/shards-node-agent',
              },
              {
                label: 'shards-cluster',
                href: 'https://github.com/damaged0ne/shards-cluster',
              },
            ],
          },
          {
            title: 'Project',
            items: [
              {
                label: 'Issues',
                href: 'https://github.com/damaged0ne/shards/issues',
              },
              {
                label: 'License (Apache-2.0)',
                href: 'https://github.com/damaged0ne/shards/blob/main/LICENSE',
              },
              {
                label: 'NOTICE',
                href: 'https://github.com/damaged0ne/shards/blob/main/NOTICE',
              },
            ],
          },
        ],
        copyright: `Copyright © ${new Date().getFullYear()} the shards contributors. Derived from Coroot (Apache-2.0); not affiliated with Coroot, Inc. Built with Docusaurus.`,
      },
      prism: {
        theme: prismThemes.github,
        darkTheme: prismThemes.dracula,
        additionalLanguages: ['java', 'bash'],
      },
    }),

  plugins: [
    [
      '@docusaurus/plugin-client-redirects',
      {
        redirects: [
          { from: '/configuration/cli-arguments', to: '/configuration/configuration' },
          { from: '/configuration/coroot-node-agent', to: '/configuration/shards-node-agent' },
          { from: '/configuration/coroot-cluster-agent', to: '/configuration/shards-cluster' },
        ],
      },
    ],
  ],
};

export default config;
