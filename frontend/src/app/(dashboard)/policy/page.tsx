import { Heading, Stack, Text } from '@orbit/terra';

export default function PolicyPage() {
  return (
    <Stack gap="spacious" asChild>
      <main className="px-6 py-8 max-w-screen-xl mx-auto">
        <Stack gap="none">
          <Heading variant="title">Policy Engine</Heading>
          <span className="mt-1 block">
            <Text color="muted">Manage grants, roles, and authorization policies.</Text>
          </span>
        </Stack>

        <Text color="subtle">
          Policy administration interface coming soon. Grants and role mappings are currently
          managed via the API.
        </Text>
      </main>
    </Stack>
  );
}
