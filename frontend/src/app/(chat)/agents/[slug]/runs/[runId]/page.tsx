import { RunShell } from './_components/run-shell';

export default async function RunRoute({
  params,
  searchParams,
}: {
  params: Promise<{ slug: string; runId: string }>;
  searchParams: Promise<{ from?: string | string[] }>;
}) {
  const [{ slug, runId }, { from }] = await Promise.all([params, searchParams]);
  return <RunShell slug={slug} runId={runId} fromChat={from === 'chat'} />;
}
