const effort = (ids: string[], def: string) => ({
  id: 'effort',
  label: 'Reasoning',
  type: 'select',
  default: def,
  choices: ids.map((id) => ({ id, label: id === 'xhigh' ? 'Extra High' : id.charAt(0).toUpperCase() + id.slice(1), default: id === def })),
})

const levels = effort(['low', 'medium', 'high', 'xhigh', 'max'], 'high')

const providers = [
  {
    driver: 'opencode',
    name: 'OpenCode',
    models: [
      { id: 'big-pickle', name: 'Big Pickle', options: [levels] },
      { id: 'ling-3', name: 'Ling 3.0 Flash Fin', options: [levels] },
      { id: 'mimo-2.6', name: 'Mimo V2.6 Flash' },
      {
        id: 'muse-1.2',
        name: 'Muse Spark 1.2 Contributor',
        options: [levels, { id: 'thinking', label: 'Thinking', type: 'boolean', default: false }, { id: 'fast', label: 'Fast Mode', type: 'boolean', default: false }],
      },
      { id: 'muse-1.3', name: 'Muse Spark 1.3 Contributor', options: [effort(['low', 'medium', 'high', 'xhigh'], 'medium')] },
      { id: 'neomotron-3', name: 'Neomotron 3 Ultra', options: [levels] },
    ],
  },
  {
    driver: 'claude',
    name: 'Claude Code',
    models: [
      { id: 'fable-5', name: 'Claude Fable 5', options: [levels] },
      { id: 'opus-5', name: 'Claude Opus 5', options: [levels] },
      { id: 'sonnet-5', name: 'Claude Sonnet 5', options: [levels] },
      { id: 'haiku-5.5', name: 'Claude Haiku 5.5', options: [levels] },
      { id: 'haiku-4.5', name: 'Claude Haiku 4.5', options: [{ id: 'thinking', label: 'Thinking', type: 'boolean', default: false }] },
    ],
  },
  { driver: 'antigravity', name: 'Antigravity', models: [{ id: 'gemini', name: 'Gemini 3 Pro', options: [effort(['low', 'high'], 'high')] }] },
]

localStorage.setItem('orchestrator_providers_cache', JSON.stringify(providers))
localStorage.setItem(
  'orchestrator_new_agent',
  JSON.stringify({ cwd: '', autoApprove: true, choice: { driver: 'opencode', model: 'muse-1.2', options: { effort: 'xhigh' } } }),
)

const titles = [
  'Can you check how the modal renders',
  'Update the dependency versions',
  'Update the modal to the new design',
  'Work on the new feature flags',
  'Can you check how auth refreshes',
  'Another chat here about the tunnel',
  'Fix the flaky git diff test',
  'Refactor the drawer gestures',
  'Add push notification badges',
  'Investigate the viewport strip',
  'Polish the glass border lighting',
  'Write docs for the phone API',
  'Speed up the thread loader',
  'Try the new effort picker',
]
const now = Date.now()
localStorage.setItem(
  'orchestrator_summaries_cache',
  JSON.stringify(
    titles.map((title, i) => ({
      threadId: `lab-${i}`,
      title,
      kind: 'agent',
      driver: 'claude',
      drivers: i % 3 === 0 ? ['claude'] : ['antigravity', 'claude'],
      model: 'opus-5',
      live: false,
      busy: i === 1,
      runningSubagents: i === 1 ? 2 : 0,
      totalSubagents: i === 1 ? 3 : 0,
      attention: i === 2 ? 'question' : '',
      lastActivity: now - i * 14_400_000,
      projectName: 'configurator',
      source: i >= 4 && i <= 5 ? 'claude-code' : undefined,
    })),
  ),
)

localStorage.setItem(
  'orchestrator_projects_cache',
  JSON.stringify([
    { path: 'C:/code/configurator', name: 'Configurator', runningAgents: 0 },
    { path: 'C:/code/home', name: 'Project Home', runningAgents: 1 },
    { path: 'C:/code/construction', name: 'Construction', runningAgents: 0 },
  ]),
)
