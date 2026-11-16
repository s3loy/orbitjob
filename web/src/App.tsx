import { OperationActivity } from './OperationActivity';
import { Box, Button, Text } from '@radix-ui/themes';
import {
  Activity,
  Gauge,
  Layers,
  List,
  Orbit,
  UserRound,
  GitBranch,
  Zap,
  Settings,
  LayoutDashboard,
  Bell,
} from 'lucide-react';
import { lazy, Suspense, useEffect, useRef, type ComponentType } from 'react';
import { Link, NavLink, Navigate, Route, Routes, useLocation } from 'react-router-dom';
import { collectionPath, returnState } from './monitoring-state';
import { useSession } from './session';
import { Empty, Loading } from './components';
import { fixtureProfile, publicDemo, projectHome } from './deployment';
import { OperationsProvider } from './operations';
import { M4Provider } from './m4-operations';
import { workspaceName } from './workspace';
import { sampleWorkspace } from './sample-workspace';
import { PageLoadBoundary } from './PageLoadBoundary';

const Connection = lazy(() =>
  import('./pages/Connection').then((m) => ({ default: m.Connection })),
);
const Overview = lazy(() => import('./pages/Overview').then((m) => ({ default: m.Overview })));
const Jobs = lazy(() => import('./pages/Jobs').then((m) => ({ default: m.Jobs })));
const JobView = lazy(() => import('./pages/Jobs').then((m) => ({ default: m.JobView })));
const Runs = lazy(() => import('./pages/Runs').then((m) => ({ default: m.Runs })));
const RunView = lazy(() => import('./pages/Runs').then((m) => ({ default: m.RunView })));
const MonitoringList = lazy(() =>
  import('./pages/Monitoring').then((m) => ({ default: m.MonitoringList })),
);
const CheckView = lazy(() => import('./pages/Monitoring').then((m) => ({ default: m.CheckView })));
const CheckResultView = lazy(() =>
  import('./pages/Monitoring').then((m) => ({ default: m.CheckResultView })),
);
const SLOView = lazy(() => import('./pages/Monitoring').then((m) => ({ default: m.SLOView })));
const SLIView = lazy(() => import('./pages/Monitoring').then((m) => ({ default: m.SLIView })));
const AlertView = lazy(() => import('./pages/Monitoring').then((m) => ({ default: m.AlertView })));
const Workflows = lazy(() => import('./pages/Workflows').then((m) => ({ default: m.Workflows })));
const WorkflowView = lazy(() =>
  import('./pages/Workflows').then((m) => ({ default: m.WorkflowView })),
);
const WorkflowRunView = lazy(() =>
  import('./pages/Workflows').then((m) => ({ default: m.WorkflowRunView })),
);
const FunctionEditor = lazy(() =>
  import('./pages/FunctionEditor').then((m) => ({ default: m.FunctionEditor })),
);
const Functions = lazy(() => import('./pages/Functions').then((m) => ({ default: m.Functions })));
const FunctionView = lazy(() =>
  import('./pages/Functions').then((m) => ({ default: m.FunctionView })),
);
const FunctionRunView = lazy(() =>
  import('./pages/Functions').then((m) => ({ default: m.FunctionRunView })),
);
const Administration = lazy(() =>
  import('./pages/Administration').then((m) => ({ default: m.Administration })),
);
const ManifestBuilder = lazy(() =>
  import('./pages/ManifestBuilder').then((m) => ({ default: m.ManifestBuilder })),
);
const DefinitionEditor = lazy(() =>
  import('./pages/DefinitionEditor').then((m) => ({ default: m.DefinitionEditor })),
);

const routes: [string, ComponentType][] = [
  ['/overview', Overview],
  ['/jobs', Jobs],
  ['/jobs/:id', JobView],
  ['/runs', Runs],
  ['/runs/:id', RunView],
  ['/checks/:id', CheckView],
  ['/check-results/:id', CheckResultView],
  ['/slos/:id', SLOView],
  ['/slis/:id', SLIView],
  ['/slo-alerts/:id', AlertView],
  ['/administration', Administration],
  ['/manifests/:kind', ManifestBuilder],
  ['/definitions/:kind', DefinitionEditor],
  ['/workflows', Workflows],
  ['/workflows/:id', WorkflowView],
  ['/workflows/:id/runs/:runId', WorkflowRunView],
  ['/functions', Functions],
  ['/functions/new', FunctionEditor],
  ['/functions/:id/edit', FunctionEditor],
  ['/functions/:id', FunctionView],
  ['/functions/:id/runs/:runId', FunctionRunView],
];
export function App() {
  return (
    <OperationsProvider>
      <M4Provider>
        <ConsoleShell />
      </M4Provider>
    </OperationsProvider>
  );
}
function ConsoleShell() {
  const session = useSession();
  const location = useLocation();
  const main = useRef<HTMLElement>(null);
  const navigation = useRef<HTMLElement>(null);
  const previousPath = useRef(location.pathname);
  const [section, detail] = location.pathname.split('/').filter(Boolean);
  const ready = Boolean(
    session.client &&
    (session.mode === 'demo' ||
      Boolean(session.identity?.target_tenant_id || session.identity?.tenant_id)),
  );
  const home = fixtureProfile ? '/jobs' : '/overview';
  const authoring = section === 'manifests' || section === 'definitions';
  const rootSection = authoring
    ? detail === 'workflow' || detail === 'workflows'
      ? 'workflows'
      : 'jobs'
    : section === 'check-results'
      ? 'checks'
      : section;
  const activeSection = rootSection === 'slis' ? 'slos' : rootSection;
  const collectionPaths = Object.fromEntries(
    (
      ['jobs', 'workflows', 'functions', 'runs', 'checks', 'slos', 'slis', 'slo-alerts'] as const
    ).map((key) => [key, collectionPath(location, key)]),
  );
  const scope =
    session.identity?.target_tenant_id || session.identity?.tenant_id || session.targetTenant;
  const workspaceLabel = workspaceName(session.identity, scope);
  const currentWorkspace = session.mode === 'demo' ? sampleWorkspace.name : workspaceLabel;
  useEffect(() => {
    if (session.client && previousPath.current !== location.pathname) {
      main.current?.focus({ preventScroll: true });
      window.scrollTo(0, 0);
    }
    const nav = navigation.current;
    if (nav && nav.scrollWidth > nav.clientWidth)
      nav.querySelector<HTMLElement>('[aria-current="page"]')?.scrollIntoView({
        block: 'nearest',
        inline: 'nearest',
      });
    previousPath.current = location.pathname;
  }, [location.pathname, session.client]);
  function protect(View: ComponentType) {
    return session.restoring ? (
      <Loading />
    ) : ready || (View === Administration && session.account?.user?.installation_admin) ? (
      <View key={`${session.generation}:${location.pathname}`} />
    ) : (
      <Navigate
        to="/connection"
        replace
        state={{ returnTo: location.pathname + location.search }}
      />
    );
  }
  const groups = [
    { label: 'Workspace', items: [['Overview', '/overview', LayoutDashboard]] },
    {
      label: 'Workloads',
      items: [
        ['Jobs', collectionPath(location, 'jobs'), Layers],
        ['Workflows', collectionPaths.workflows, GitBranch],
        ['Functions', collectionPaths.functions, Zap],
      ],
    },
    { label: 'Execution history', items: [['Runs', collectionPaths.runs, List]] },
    {
      label: 'Reliability',
      items: [
        ['Checks', collectionPath(location, 'checks'), Activity],
        ['SLOs', collectionPath(location, 'slos'), Gauge],
        ['Alerts', collectionPaths['slo-alerts'], Bell],
      ],
    },
  ] as const;
  return (
    <div className={`app-shell${!ready ? ' setup-shell' : ''}`}>
      <a className="skip-link" href="#main">
        Skip to content
      </a>
      {!ready && (
        <Link to="/connection" className="brand setup-brand">
          <Orbit size={22} />
          OrbitJob
        </Link>
      )}
      {ready && (
        <aside className="sidebar">
          <div className="sidebar-brand">
            <Link to={home} className="brand">
              <Orbit size={22} />
              OrbitJob
            </Link>
            {session.mode === 'demo' && (
              <Text size="1" className="connection-state" title="Changes stay in this session">
                Sample data
              </Text>
            )}
          </div>
          <div className="workspace" aria-label="Current workspace">
            <Text as="p" size="2" weight="medium">
              <Link
                to="/connection"
                className="workspace-context-link"
                aria-label={`Review ${currentWorkspace} workspace`}
              >
                {currentWorkspace}
              </Link>
            </Text>
          </div>
          {ready && (
            <nav aria-label="Main navigation" ref={navigation}>
              {groups.map((group) => (
                <div className="nav-group" key={group.label}>
                  <Text as="p" size="1" className="nav-group-label">
                    {group.label}
                  </Text>
                  {group.items.map(([name, path, Icon]) => (
                    <Link
                      key={name}
                      to={path}
                      state={returnState(location.state, path)}
                      className={`nav-item ${path.split('?')[0] === `/${activeSection}` ? 'active' : ''}`}
                      aria-current={path.split('?')[0] === `/${activeSection}` ? 'page' : undefined}
                    >
                      <Icon size={17} />
                      {name}
                    </Link>
                  ))}
                </div>
              ))}
            </nav>
          )}
          <div className="sidebar-bottom">
            {publicDemo && (
              <a href={projectHome} className="nav-item">
                Project website
              </a>
            )}
            {ready && (
              <NavLink
                to="/administration"
                className={({ isActive }) => `nav-item ${isActive ? 'active' : ''}`}
              >
                <Settings size={17} />
                Administration
              </NavLink>
            )}
            <NavLink
              to="/connection"
              className={({ isActive }) => `nav-item ${isActive ? 'active' : ''}`}
            >
              <UserRound size={17} />
              {session.mode === 'demo' || publicDemo ? 'Workspace' : 'Account'}
            </NavLink>
          </div>
        </aside>
      )}
      <div className="main-shell">
        <main id="main" tabIndex={-1} ref={main}>
          <OperationActivity />
          <PageLoadBoundary
            key={`${session.generation}:${location.pathname}`}
            home={location.pathname === home ? '/jobs' : home}
            connected={ready}
            live={session.mode === 'live'}
          >
            <Suspense fallback={<Loading />}>
              <Routes>
                <Route path="/connection" element={<Connection />} />
                <Route
                  path="/"
                  element={
                    session.restoring ? (
                      <Loading />
                    ) : (
                      <Navigate to={ready ? home : '/connection'} replace />
                    )
                  }
                />
                {routes.map(([path, View]) => (
                  <Route key={path} path={path} element={protect(View)} />
                ))}
                {(['checks', 'slos', 'slis', 'slo-alerts'] as const).map((resource) => (
                  <Route
                    key={resource}
                    path={`/${resource}`}
                    element={
                      session.restoring ? (
                        <Loading />
                      ) : ready ? (
                        <MonitoringList key={resource} resource={resource} />
                      ) : (
                        <Navigate
                          to="/connection"
                          replace
                          state={{ returnTo: location.pathname + location.search }}
                        />
                      )
                    }
                  />
                ))}
                <Route
                  path="*"
                  element={
                    <Box>
                      <Empty title="Page not found">This console route does not exist.</Empty>
                      <Button asChild size="1">
                        <Link to={home}>Back to workspace</Link>
                      </Button>
                    </Box>
                  }
                />
              </Routes>
            </Suspense>
          </PageLoadBoundary>
        </main>
      </div>
    </div>
  );
}
