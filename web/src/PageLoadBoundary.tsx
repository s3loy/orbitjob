import { Component, createRef, type ErrorInfo, type ReactNode } from 'react';
import { Button, Callout, Flex, Heading, Text } from '@radix-ui/themes';
import { CircleAlert } from 'lucide-react';
import { Link } from 'react-router-dom';

type Props = {
  children: ReactNode;
  home: string;
  connected: boolean;
  live: boolean;
};

// Route failures must not unmount the connection, navigation or operation store.
// A rejected module import can remain cached for this document, so recovery
// offers navigation and an explicit reload instead of promising an import retry.
export class PageLoadBoundary extends Component<Props, { failed: boolean }> {
  state = { failed: false };
  private notice = createRef<HTMLDivElement>();

  static getDerivedStateFromError() {
    return { failed: true };
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error('Console page could not be displayed.', {
      name: error.name,
      componentStack: info.componentStack,
    });
    this.notice.current?.focus();
  }

  render() {
    if (!this.state.failed) return this.props.children;
    return (
      <section aria-label="Page recovery">
        <Heading as="h1" size="5" mb="4">
          Page unavailable
        </Heading>
        <Callout.Root color="red" role="alert" ref={this.notice} tabIndex={-1}>
          <Callout.Icon>
            <CircleAlert size={16} />
          </Callout.Icon>
          <Callout.Text>
            This page could not be displayed. Navigation and Activity remain available. Use the
            navigation to continue or reopen a draft.
          </Callout.Text>
        </Callout.Root>
        {this.props.connected && (
          <Flex mt="4">
            <Button asChild size="2" variant="outline">
              <Link to={this.props.home}>Open workspace</Link>
            </Button>
          </Flex>
        )}
        <details className="advanced-details">
          <summary>Reload options</summary>
          <Text as="p" size="2" mb="3">
            Reloading discards changes kept only in this tab and clears Activity.
            {this.props.live && ' Your sign-in is restored when the server is reachable.'}
          </Text>
          <Text as="p" size="2" mb="3">
            Before reloading, open Activity and record any pending or unknown request IDs and
            receipts. Accepted work continues independently; losing its local receipt does not undo
            the request.
          </Text>
          <Button size="2" variant="outline" color="red" onClick={() => window.location.reload()}>
            Reload and clear local state
          </Button>
        </details>
      </section>
    );
  }
}
