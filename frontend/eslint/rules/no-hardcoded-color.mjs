// Matches a full string that IS a hex color: '#f472b6', '#fff', '#rrggbbaa'.
const HEX_FULL = /^#[0-9a-fA-F]{3,8}$/;

// Matches a full string that IS a raw color function: 'oklch(…)', 'rgb(…)', etc.
// A leading 'color-mix(' is intentionally excluded — it is a derived value and
// may legitimately reference tokens via var(). We catch the raw-function case
// via HEX_FULL / HEX_EMBEDDED / RAW_FN_EMBEDDED when the inner values are raw.
const RAW_FN_FULL = /^(?:oklch|rgb|rgba|hsl|hsla)\s*\(/i;

// Matches a hex colour embedded anywhere in a longer string (e.g. box-shadow values).
// Requires exactly 3, 6, or 8 hex digits and a non-hex char (or end) after, to
// avoid partial matches on longer sequences.
const HEX_EMBEDDED = /#(?:[0-9a-fA-F]{8}|[0-9a-fA-F]{6}|[0-9a-fA-F]{3})(?![0-9a-fA-F])/;

// Matches a raw color function embedded anywhere in a longer string.
// Requires a digit, dot, or '%' as the first character inside the parens to
// distinguish real calls like 'oklch(60% 0.15 250)' from the 'in oklch,'
// fragment inside 'color-mix(in oklch, var(--...) N%, transparent)'.
const RAW_FN_EMBEDDED = /(?:oklch|rgb|rgba|hsl|hsla)\s*\(\s*[\d.%]/i;

function isRawColor(value) {
  if (typeof value !== 'string') return false;
  // Full-string checks first (cheap).
  if (HEX_FULL.test(value) || RAW_FN_FULL.test(value)) return true;
  // Embedded checks for compound values (e.g. box-shadow, multi-stop gradients).
  return HEX_EMBEDDED.test(value) || RAW_FN_EMBEDDED.test(value);
}

const rule = {
  meta: {
    type: 'problem',
    docs: {
      description:
        'Disallow hardcoded colour literals (hex, oklch, rgb, hsl). ' +
        'All colours must reference a Hades CSS variable so they respond to the active theme.',
    },
    messages: {
      noHardcodedColor:
        'Hardcoded colour value "{{value}}" is not allowed. ' +
        'Use a Hades CSS variable — e.g. var(--color-foreground), var(--color-chart-1) — ' +
        'or derive from one via color-mix(). ' +
        'Inline colour literals are not themed and break in light mode. ' +
        'See frontend/src/packages/hades/src/tokens/tokens.css for the full token list.',
    },
    schema: [],
  },
  create(context) {
    return {
      Literal(node) {
        if (typeof node.value !== 'string') return;
        if (!isRawColor(node.value)) return;
        context.report({
          node,
          messageId: 'noHardcodedColor',
          data: {
            // Truncate long compound values (e.g. multi-layer box-shadows) in the message.
            value: node.value.length > 60 ? node.value.slice(0, 57) + '…' : node.value,
          },
        });
      },
    };
  },
};

export default rule;
