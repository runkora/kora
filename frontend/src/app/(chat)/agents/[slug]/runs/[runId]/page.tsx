import { RunShell } from './_components/run-shell';

export default async function RunPage({
  params,
}: {
  params: Promise<{ slug: string; runId: string }>;
}) {
  const { slug, runId } = await params;
  return <RunShell slug={slug} runId={runId} />;
}
