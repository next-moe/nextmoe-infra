import withNuxt from './.nuxt/eslint.config.mjs'
import a11y from 'eslint-plugin-vuejs-accessibility'

export default withNuxt(
  // Auto-generated tool output — never lint/format (regenerate, don't edit):
  // the openapi-typescript types, whose CI drift gate diffs the tool's raw
  // output, and the inline-SVG icon registry, whose JSON.stringify quoting
  // `eslint --fix` would rewrite and the next `pnpm icons` would undo.
  { ignores: ['shared/types/generated/**', 'app/assets/kun-icons.ts'] },
  // Apply the a11y plugin's recommended rules to all .vue files.
  // Documentation: https://vue-a11y.github.io/eslint-plugin-vuejs-accessibility/
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
      'space-before-function-paren': 'off',
      'func-call-spacing': 'off',
      quotes: [
        'error',
        'single',
        { avoidEscape: true, allowTemplateLiterals: true }
      ],
      // A11y plugin tuning — relax rules that conflict with the
      // existing design system or that we explicitly handle elsewhere.
      // Tab/Modal/etc. are accessible via focus trap + keyboard nav,
      // but the lint plugin can't statically prove it.
      'vuejs-accessibility/no-autofocus': 'off',
      'vuejs-accessibility/click-events-have-key-events': 'warn',
      'vuejs-accessibility/no-static-element-interactions': 'warn'
    }
  }
)
