import withNuxt from './.nuxt/eslint.config.mjs'
import a11y from 'eslint-plugin-vuejs-accessibility'

export default withNuxt(
  // Tool output, regenerated rather than edited: the openapi-typescript types,
  // the inline-SVG icon registry written by scripts/generate-icons.mjs, and the
  // guideline docs compiled by scripts/build-guidelines.mjs.
  {
    ignores: [
      'shared/types/generated/**',
      'app/assets/kun-icons.ts',
      'app/generated/**'
    ]
  },
  a11y.configs['flat/recommended'],
  {
    rules: {
      'no-console': 'off',
      camelcase: 'off',
      'comma-spacing': 'off',
      '@typescript-eslint/no-unused-vars': 'off',
      'vue/multi-word-component-names': 'off',
      'vue/singleline-html-element-content-newline': 'off',
      'vue/html-self-closing': 'off',
      'vue/attributes-order': 'off',
      'vue/no-multiple-template-root': 'off',
      'vue/no-v-html': 'off',
      'import/order': 'off',
      'import/no-named-as-default-member': 'off',
      'arrow-parens': ['error', 'always'],
      'func-style': ['error', 'expression'],
      'space-before-function-paren': 'off',
      'func-call-spacing': 'off',
      quotes: [
        'error',
        'single',
        { avoidEscape: true, allowTemplateLiterals: true }
      ],
      'vuejs-accessibility/no-autofocus': 'off',
      'vuejs-accessibility/click-events-have-key-events': 'warn',
      'vuejs-accessibility/no-static-element-interactions': 'warn'
    }
  }
)
