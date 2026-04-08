export { authHandlers } from './auth';
export { healthHandlers } from './health';

import { authHandlers } from './auth';
import { healthHandlers } from './health';

/** All default MSW handlers. Passed to `setupServer` in msw/server.ts. */
export const defaultHandlers = [...authHandlers, ...healthHandlers];
