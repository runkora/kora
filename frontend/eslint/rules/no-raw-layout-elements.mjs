const rule = {
  meta: {
    type: 'problem',
    docs: {
      description:
        'Disallow raw <div>, <section>, <header>, <article>, <nav>, <aside>, and <footer> ' +
        'elements in product JSX. Use <Stack>, <Grid>, <Section>, <Callout>, <Navigation>, ' +
        '<SidePanel>, <TableToolbarShell>, or PageLayout slots from @orbit/hades instead. ' +
        'See frontend/docs/reference/hades-design-system.md',
    },
    messages: {
      noRawDiv:
        'Raw <div> is not allowed. Use <Stack> (flex), <Grid> (grid), or <Section> from @orbit/hades. ' +
        'Do not suppress this error with a disable comment. If no Hades component covers this need, write a proposal for human engineer approval before modifying Hades. ' +
        'See frontend/docs/reference/hades-design-system.md',
      noRawSection:
        'Raw <section> is not allowed. Use <Section> from @orbit/hades. ' +
        'For layout-only grouping, use <Stack>. ' +
        'See frontend/docs/reference/hades-design-system.md',
      noRawHeader:
        'Raw <header> is not allowed. Use <TableToolbarShell> for toolbar containers, ' +
        '<SidePanelHeader> for panel headers, or <Stack> for generic grouping (from @orbit/hades). ' +
        'See frontend/docs/reference/hades-design-system.md',
      noRawArticle:
        'Raw <article> is not allowed. Use <Card> for content cards, <Stack> for grouping, ' +
        'or <SidePanelBody> for panel body sections (from @orbit/hades). ' +
        'See frontend/docs/reference/hades-design-system.md',
      noRawNav:
        'Raw <nav> is not allowed. Use <Navigation> for sidebar nav landmarks (from @orbit/hades). ' +
        'For breadcrumb trails, use <Stack aria-label="Breadcrumb">. ' +
        'See frontend/docs/reference/hades-design-system.md',
      noRawAside:
        'Raw <aside> is not allowed. Use <Callout> for informational callout boxes, ' +
        '<SidePanel> for slide-over panels, or <Stack> for generic grouping (from @orbit/hades). ' +
        'See frontend/docs/reference/hades-design-system.md',
      noRawFooter:
        'Raw <footer> is not allowed. Use the PageLayout sidebarFooter prop for sidebar footers, ' +
        '<SidePanelFooter> for panel footers, or <Stack> for generic grouping (from @orbit/hades). ' +
        'See frontend/docs/reference/hades-design-system.md',
      noRawLayoutElement:
        'Raw <{{tag}} className="{{cls}} …"> uses <{{tag}}> as a layout container. ' +
        'Use the appropriate Hades component instead. ' +
        'See frontend/docs/reference/hades-design-system.md',
    },
    schema: [],
  },
  create(context) {
    const LAYOUT_CLASSES = new Set([
      'flex',
      'inline-flex',
      'grid',
      'inline-grid',
      'block',
      'inline-block',
    ]);
    // Only <main> remains in the conditional set — it has no Hades equivalent yet.
    const SEMANTIC_ELEMENTS = new Set(['main']);
    return {
      JSXOpeningElement(node) {
        if (node.name.type !== 'JSXIdentifier') return;
        const tag = node.name.name;
        // <div> fires unconditionally — no className check needed.
        if (tag === 'div') {
          context.report({ node, messageId: 'noRawDiv' });
          return;
        }
        // <section> fires unconditionally — Hades provides a Section wrapper.
        if (tag === 'section') {
          context.report({ node, messageId: 'noRawSection' });
          return;
        }
        // The following five fire unconditionally — Hades provides compliant
        // alternatives for all product-code use cases.
        if (tag === 'header') {
          context.report({ node, messageId: 'noRawHeader' });
          return;
        }
        if (tag === 'article') {
          context.report({ node, messageId: 'noRawArticle' });
          return;
        }
        if (tag === 'nav') {
          context.report({ node, messageId: 'noRawNav' });
          return;
        }
        if (tag === 'aside') {
          context.report({ node, messageId: 'noRawAside' });
          return;
        }
        if (tag === 'footer') {
          context.report({ node, messageId: 'noRawFooter' });
          return;
        }
        if (!SEMANTIC_ELEMENTS.has(tag)) return;
        const classNameAttr = node.attributes.find(
          (attr) =>
            attr.type === 'JSXAttribute' &&
            attr.name.type === 'JSXIdentifier' &&
            attr.name.name === 'className',
        );
        if (!classNameAttr) return;
        const value = classNameAttr.value;
        // Only inspect static string literals — template literals and
        // expressions are too dynamic to evaluate reliably at lint time.
        if (!value || value.type !== 'Literal' || typeof value.value !== 'string') return;
        const classes = value.value.split(/\s+/);
        const hit = classes.find((cls) => LAYOUT_CLASSES.has(cls));
        if (hit) {
          context.report({ node, messageId: 'noRawLayoutElement', data: { cls: hit, tag } });
        }
      },
    };
  },
};

export default rule;
