import { Button, Flex, Text } from '@radix-ui/themes';
import { Link, useLocation } from 'react-router-dom';
import { dismissMonitoringCreation, useMonitoringCreations } from './monitoring-creations';
import { forwardState } from './monitoring-state';
import { useSession } from './session';
import type { Resource } from './api/monitoring';

export function MonitoringReceipts({ resource, id }: { resource?: Resource; id?: string }) {
  const { client } = useSession();
  const location = useLocation();
  const matching = useMonitoringCreations(client).filter(
    (item) => (!resource || item.resource === resource) && (!id || item.id === id),
  );
  const receipts = resource && !id ? matching.slice(0, 1) : matching;
  if (!receipts.length) return null;
  return (
    <section aria-label="Monitoring creation receipts">
      {receipts.map((item) => (
        <div key={item.key} className="operation-notice monitoring-receipt" role="status">
          <Text as="p" size="2" weight="medium">
            Created {item.name}
          </Text>
          <Text as="p" size="1">
            {item.resource === 'checks' ? 'Check' : item.resource === 'slis' ? 'SLI' : 'SLO'} #
            {item.id}
          </Text>
          <Flex gap="3" mt="2" wrap="wrap">
            {location.pathname !== `/${item.resource}/${item.id}` && (
              <Link
                className="record-link"
                to={`/${item.resource}/${item.id}`}
                state={forwardState(location)}
              >
                Open saved resource
              </Link>
            )}
            <Button
              size="1"
              variant="ghost"
              onClick={() => client && dismissMonitoringCreation(client, item.key)}
            >
              Dismiss
            </Button>
          </Flex>
        </div>
      ))}
    </section>
  );
}
