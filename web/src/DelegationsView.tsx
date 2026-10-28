import { useCallback, useEffect, useState } from 'react';
import { createDelegation, endDelegation, listDelegations, listPrincipals, type Delegation, type Principal } from './api';
import { OutcomeLine, ReasonForm, type Outcome } from './PrincipalsView';

function message(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

// DelegationsView lets admins allow an agent to act for a human. The agent
// then holds only the roles both of them hold (SPEC-006-R08).
export default function DelegationsView() {
  const [delegations, setDelegations] = useState<Delegation[] | null>(null);
  const [principals, setPrincipals] = useState<Principal[]>([]);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [creating, setCreating] = useState(false);
  const [ending, setEnding] = useState<Delegation | null>(null);
  const [outcome, setOutcome] = useState<Outcome>(null);

  const reload = useCallback(() => {
    Promise.all([listDelegations(), listPrincipals()])
      .then(([d, p]) => {
        setDelegations(d.items);
        setPrincipals(p.items);
      })
      .catch((err: unknown) => setLoadError(message(err)));
  }, []);

  useEffect(reload, [reload]);

  function done(text: string) {
    setCreating(false);
    setEnding(null);
    setOutcome({ ok: true, text });
    reload();
  }

  if (loadError) return <p role="alert" className="error">{loadError}</p>;
  if (!delegations) return <p className="muted">Loading delegations…</p>;

  const agents = principals.filter((p) => p.kind === 'agent' && p.enabled);
  const humans = principals.filter((p) => p.kind === 'human' && p.enabled);

  return (
    <article className="view">
      <h1>Agent delegations</h1>
      <p className="muted">An agent acts only for a human who has been delegated to it, and never beyond that human's roles. Its actions are recorded as "human via agent".</p>
      <p>
        <button
          type="button"
          onClick={() => {
            setOutcome(null);
            setEnding(null);
            setCreating(true);
          }}
        >
          New delegation
        </button>
      </p>
      <OutcomeLine outcome={outcome} />
      {creating && <CreateForm agents={agents} humans={humans} onDone={done} onCancel={() => setCreating(false)} />}
      {ending && (
        <ReasonForm
          title={`End delegation ${ending.human_handle} → ${ending.agent_handle}`}
          submitLabel="End delegation"
          onCancel={() => setEnding(null)}
          onSubmit={(reason) => endDelegation(ending.id, reason).then(() => done(`${ending.agent_handle} can no longer act for ${ending.human_handle}.`))}
        />
      )}

      {delegations.length === 0 ? (
        <p className="muted">No active delegations.</p>
      ) : (
        <table className="data">
          <caption className="sr-only">Active delegations</caption>
          <thead>
            <tr>
              <th scope="col">Human</th>
              <th scope="col">Agent</th>
              <th scope="col">Reason</th>
              <th scope="col">Since</th>
              <th scope="col">Actions</th>
            </tr>
          </thead>
          <tbody>
            {delegations.map((d) => (
              <tr key={d.id}>
                <th scope="row" className="mono">
                  {d.human_handle}
                </th>
                <td className="mono">{d.agent_handle}</td>
                <td>{d.reason}</td>
                <td>{new Date(d.created_at).toLocaleString()}</td>
                <td>
                  <button
                    type="button"
                    aria-label={`End delegation from ${d.human_handle} to ${d.agent_handle}`}
                    onClick={() => {
                      setOutcome(null);
                      setCreating(false);
                      setEnding(d);
                    }}
                  >
                    End
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </article>
  );
}

function CreateForm({ agents, humans, onDone, onCancel }: { agents: Principal[]; humans: Principal[]; onDone: (text: string) => void; onCancel: () => void }) {
  const [agent, setAgent] = useState(agents[0]?.id ?? '');
  const [human, setHuman] = useState(humans[0]?.id ?? '');
  if (agents.length === 0 || humans.length === 0) {
    return (
      <div className="action-form">
        <p>A delegation needs an enabled agent and an enabled human. Create them under Principals first.</p>
        <button type="button" onClick={onCancel}>
          Close
        </button>
      </div>
    );
  }
  const handle = (list: Principal[], id: string) => list.find((p) => p.id === id)?.handle ?? id;
  return (
    <ReasonForm
      title="New delegation"
      submitLabel="Delegate"
      onCancel={onCancel}
      onSubmit={(reason) => createDelegation(agent, human, reason).then(() => onDone(`${handle(agents, agent)} can now act for ${handle(humans, human)}.`))}
    >
      <label htmlFor="del-human">Human</label>
      <select id="del-human" value={human} onChange={(e) => setHuman(e.target.value)}>
        {humans.map((p) => (
          <option key={p.id} value={p.id}>
            {p.handle}
          </option>
        ))}
      </select>
      <label htmlFor="del-agent">Agent</label>
      <select id="del-agent" value={agent} onChange={(e) => setAgent(e.target.value)}>
        {agents.map((p) => (
          <option key={p.id} value={p.id}>
            {p.handle}
          </option>
        ))}
      </select>
    </ReasonForm>
  );
}
