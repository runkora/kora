// Stands in for policy/_actions/grants.ts in Storybook. The real module is a server action file
// whose import chain reaches `server-only`, which throws when bundled for the browser.
type Result<T> = { ok: true; data: T } | { ok: false; status: number; message: string };

const unavailable = <T>(): Result<T> => ({
  ok: false,
  status: 503,
  message: 'Not available in Storybook.',
});

export async function createGrantAction(): Promise<Result<never>> {
  return unavailable();
}
export async function updateGrantBudgetAction(): Promise<Result<never>> {
  return unavailable();
}
export async function deleteGrantAction(): Promise<Result<void>> {
  return unavailable();
}
export async function searchUsersAction(): Promise<
  Result<{ id: string; email: string; display_name: string }[]>
> {
  return {
    ok: true,
    data: [
      { id: 'usr_42b7e9', email: 'dev@kora.example', display_name: 'Dev Operator' },
      { id: 'usr_9d51a3', email: 'maya.chen@kora.example', display_name: 'Maya Chen' },
    ],
  };
}
