import requireServerOnly from './rules/require-server-only.mjs';
import requireUseClientForHooks from './rules/require-use-client-for-hooks.mjs';
import noTerraClassname from './rules/no-terra-classname.mjs';
import noRawHtmlTextTags from './rules/no-raw-html-text-tags.mjs';
import noRawLayoutElements from './rules/no-raw-layout-elements.mjs';
import noRawInteractiveElements from './rules/no-raw-interactive-elements.mjs';
import noRawListElements from './rules/no-raw-list-elements.mjs';
import noRawFormElements from './rules/no-raw-form-elements.mjs';
import noRawInlineElements from './rules/no-raw-inline-elements.mjs';
import requireKebabFilename from './rules/require-kebab-filename.mjs';
import noTerraDisableComment from './rules/no-terra-disable-comment.mjs';
import noDirectLucideImport from './rules/no-direct-lucide-import.mjs';
import noInlineClipboardWrite from './rules/no-inline-clipboard-write.mjs';

const localPlugin = {
  rules: {
    'require-server-only': requireServerOnly,
    'require-use-client-for-hooks': requireUseClientForHooks,
    'no-terra-classname': noTerraClassname,
    'no-raw-html-text-tags': noRawHtmlTextTags,
    'no-raw-layout-elements': noRawLayoutElements,
    'no-raw-interactive-elements': noRawInteractiveElements,
    'no-raw-list-elements': noRawListElements,
    'no-raw-form-elements': noRawFormElements,
    'no-raw-inline-elements': noRawInlineElements,
    'require-kebab-filename': requireKebabFilename,
    'no-terra-disable-comment': noTerraDisableComment,
    'no-direct-lucide-import': noDirectLucideImport,
    'no-inline-clipboard-write': noInlineClipboardWrite,
  },
};

export default localPlugin;
