import { ChatPage } from './_components/chat-page';

export default async function ChatRoute({
  searchParams,
}: {
  searchParams: Promise<{ agent?: string | string[] }>;
}) {
  const { agent } = await searchParams;
  return <ChatPage agentSlug={typeof agent === 'string' ? agent : undefined} />;
}
