import { Link, useLocation } from 'react-router-dom';
import { Activity, KeyRound } from 'lucide-react';
import { useAdministrationCredential, useAdministrationCreations } from './administration-state';
import { useSession } from './session';

// One compact, non-secret reminder survives navigation. Full results live in
// Administration; the key panel already owns its pending/revealed instructions.
export function AdministrationReceipt() {
  const { client } = useSession();
  const location = useLocation();
  const operation = useAdministrationCredential(client);
  const receipts = useAdministrationCreations(client);
  const active = ['pending', 'revealed', 'unknown'].includes(operation.stage);
  const administration = location.pathname === '/administration';
  if (!active && (!receipts.length || administration)) return null;
  return (
    <div
      className="administration-operation"
      role="status"
      aria-label={active ? 'API key operation' : 'Recent changes'}
    >
      {active ? <KeyRound size={14} /> : <Activity size={14} />}
      {active && (
        <>
          {!administration && (
            <span>
              {operation.stage === 'pending'
                ? 'API key creation is pending.'
                : operation.stage === 'revealed'
                  ? 'An API key is ready to save.'
                  : 'API key creation needs reconciliation.'}
            </span>
          )}
          <Link to="/administration?review=key">Review API key operation</Link>
        </>
      )}
      {receipts.length > 0 && !administration && (
        <Link to="/administration?review=created#administration-activity">
          View recent changes ({receipts.length})
        </Link>
      )}
    </div>
  );
}
