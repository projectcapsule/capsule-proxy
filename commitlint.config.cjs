const Configuration = {
    extends: ['@commitlint/config-conventional'],
    plugins: ['commitlint-plugin-function-rules'],
    rules: {
      'type-enum': [2, 'always', ['chore', 'ci', 'docs', 'feat', 'test', 'fix', 'sec']],
      'body-max-line-length': [1, 'always', 500],
    },
    /*
     * Whether commitlint uses the default ignore rules, see the description above.
     */
    defaultIgnores: true,
    /*
     * Custom URL to show upon failure
     */
    helpUrl:
      'https://github.com/projectcapsule/capsule-proxy/blob/main/CONTRIBUTING.md#commits',
  };
  
  module.exports = Configuration;