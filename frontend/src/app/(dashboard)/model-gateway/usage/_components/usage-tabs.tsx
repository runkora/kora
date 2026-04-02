'use client';

import { Tabs, TabsList, TabsTrigger, TabsContent } from '@orbit/terra';
import { SpendChart } from './spend-chart';
import { TokensChart } from './tokens-chart';
import { RequestsChart } from './requests-chart';

export function UsageTabs() {
  return (
    <Tabs defaultValue="spend">
      <TabsList>
        <TabsTrigger value="spend">Spend</TabsTrigger>
        <TabsTrigger value="tokens">Tokens</TabsTrigger>
        <TabsTrigger value="requests">Requests</TabsTrigger>
      </TabsList>

      <TabsContent value="spend">
        <SpendChart />
      </TabsContent>

      <TabsContent value="tokens">
        <TokensChart />
      </TabsContent>

      <TabsContent value="requests">
        <RequestsChart />
      </TabsContent>
    </Tabs>
  );
}
