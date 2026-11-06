import React, { Suspense, useCallback, useEffect, useRef, useState } from 'react';
import { useWallpaper } from '../sidebar/useWallpaper';
import { useProjects } from '../orchestrator/useProjects';
import { useSpawner } from '../orchestrator/useSpawner';
import { useAttachments } from '../common/useAttachments';
import { useHistory } from '../history/useHistory';
import { useUsage } from '../sidebar/useUsage';
import { useClaudeImport } from '../history/useClaudeImport';
import { useDefaultModels } from '../sidebar/useDefaultModels';
import { useTheme } from '../../themes';
import { useOrchestratorStore } from '../orchestrator/useOrchestratorStore';
import { AgentWindow } from '../agent-session';
import { NavRail } from '../layout/NavRail';
import { HomeStats } from '../layout/HomeStats';
import { GlassInput } from '../layout/GlassInput';
import { DynamicIsland, DynamicIslandNotification } from '../common/DynamicIsland';
import { SettingsPanel } from '../sidebar/SettingsPanel';
import { RespondToQuestion } from '../../../wailsjs/go/main/App';
import { EventsOn } from '../../../wailsjs/runtime/runtime';
import { playTaskComplete } from '../../sound';
import { domain } from '../../../wailsjs/go/models';

export interface SceneProps {
  children?: React.ReactNode;
}

export const Scene: React.FC<SceneProps> = () => {
  const wallpaper = useWallpaper();
  const projects = useProjects();
  const spawner = useSpawner();
  const attachments = useAttachments();
  const { currentTheme } = useTheme();
  const isLight = currentTheme.id === 'light' || currentTheme.id === 'white';

  // Panels & Triggers
  const [isSettingsOpen, setIsSettingsOpen] = useState(false);
  const [projectsTrigger, setProjectsTrigger] = useState(0);
  const [historyTrigger, setHistoryTrigger] = useState(0);

  const defaultModels = useDefaultModels(isSettingsOpen);
  const history = useHistory(true);
  const usage = useUsage(true);
  const claudeImport = useClaudeImport();
  const activeTasks = spawner.tasks;

  // View state: 'home' (blank chat screen) or 'chat' (active fullscreen chat)
  const [activeView, setActiveView] = useState<'home' | 'chat'>('home');
  const [activeCardIndex, setActiveCardIndex] = useState(0);

  // Dynamic Island Notifications
  const [islandNotification, setIslandNotification] = useState<DynamicIslandNotification | null>(null);
  const notificationTimerRef = useRef<number | null>(null);

  // Active task object
  const activeTask = activeTasks[activeCardIndex] ?? null;

  // Play completion sound and surface notification in Dynamic Island
  useEffect(() => {
    const off = EventsOn(
      'session:notification',
      (notif: { type: string; title: string; message: string; threadId?: string }) => {
        if (notificationTimerRef.current) {
          window.clearTimeout(notificationTimerRef.current);
        }
        if (notif.type === 'success' && useOrchestratorStore.getState().interfaceSounds) {
          playTaskComplete();
        }
        setIslandNotification({
          id: `${Date.now()}-${Math.random().toString(36).slice(2, 6)}`,
          timestamp: Date.now(),
          title: notif.title || 'Completed',
          subtitle: notif.message,
          type: notif.type === 'alert' ? 'alert' : 'success',
          threadId: notif.threadId,
        });
        notificationTimerRef.current = window.setTimeout(() => {
          setIslandNotification(null);
        }, 5000);
      },
    );
    return () => {
      off();
    };
  }, []);

  // When a new agent is spawned, automatically navigate to it in full-bleed chat
  const spawnedCount = activeTasks.length;
  const prevSpawnedCount = useRef(spawnedCount);
  useEffect(() => {
    if (spawnedCount > prevSpawnedCount.current) {
      setActiveCardIndex(spawnedCount - 1);
      setActiveView('chat');
    }
    prevSpawnedCount.current = spawnedCount;
  }, [spawnedCount]);

  // Keyboard navigation:
  // - Ctrl + ArrowLeft: previous agent session (or return to home)
  // - Ctrl + ArrowRight: next agent session (or enter active chat from home)
  // - Ctrl + N: new blank chat / home
  // - Escape: close settings
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && (e.key === 'n' || e.key === 'N')) {
        e.preventDefault();
        setActiveView('home');
        return;
      }

      if ((e.ctrlKey || e.metaKey) && e.key === 'ArrowRight') {
        e.preventDefault();
        if (activeView === 'home' && activeTasks.length > 0) {
          setActiveCardIndex(0);
          setActiveView('chat');
        } else if (activeView === 'chat') {
          if (activeCardIndex < activeTasks.length - 1) {
            setActiveCardIndex((prev) => prev + 1);
          }
        }
        return;
      }

      if ((e.ctrlKey || e.metaKey) && e.key === 'ArrowLeft') {
        e.preventDefault();
        if (activeView === 'chat') {
          if (activeCardIndex > 0) {
            setActiveCardIndex((prev) => prev - 1);
          } else {
            setActiveView('home');
          }
        }
        return;
      }

      if (e.key === 'Escape') {
        setIsSettingsOpen(false);
        return;
      }
    };

    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [activeTasks.length, activeCardIndex, activeView]);

  // Account label helper
  const providers = useOrchestratorStore((state) => state.providers);
  const accountLabel = useCallback(
    (driver?: string, account?: string) => {
      const accounts = providers.find((p) => p.id === driver)?.accounts ?? [];
      if (accounts.length < 2) return undefined;
      return accounts.find((entry) => entry.id === (account || 'default'))?.name;
    },
    [providers],
  );

  // Submit prompt from GlassInput (Home view)
  const handleSubmitPrompt = async (promptText: string, modelId?: string) => {
    const trimmed = promptText.trim();
    const refs = attachments.toRefs();
    if (!trimmed && refs.length === 0) return;
    const threadId = await spawner.spawnAgent(
      trimmed,
      undefined,
      modelId,
      projects.active ? { name: projects.active.name, path: projects.active.path } : undefined,
      refs,
    );
    attachments.release();
    if (threadId) {
      setActiveView('chat');
    }
  };

  const handleCloseTask = (threadId: string) => {
    spawner.close(threadId);
    if (activeTasks.length <= 1) {
      setActiveView('home');
      setActiveCardIndex(0);
    } else if (activeCardIndex >= activeTasks.length - 1) {
      setActiveCardIndex(Math.max(0, activeCardIndex - 1));
    }
  };

  const handleOpenHistorySession = async (workspaceId: string, threadId?: string) => {
    const targetIndex = await spawner.openHistorySession(workspaceId, threadId);
    if (targetIndex >= 0) {
      setActiveCardIndex(targetIndex);
      setActiveView('chat');
    }
  };

  return (
    <div
      className={`relative w-screen h-screen overflow-hidden flex select-none ${
        isLight ? 'bg-[#f4f4f7] text-black' : 'bg-[#060709] text-white'
      }`}
    >
      {/* Subtle Wallpaper Layer with 0% top to 8% bottom opacity mask */}
      <div
        className="absolute inset-0 pointer-events-none z-0 overflow-hidden transition-opacity duration-700"
        style={{
          backgroundImage: wallpaper.selected ? `url(${wallpaper.selected.url})` : undefined,
          backgroundSize: 'cover',
          backgroundPosition: 'center',
          maskImage: 'linear-gradient(to bottom, rgba(0, 0, 0, 0) 0%, rgba(0, 0, 0, 0.08) 100%)',
          WebkitMaskImage: 'linear-gradient(to bottom, rgba(0, 0, 0, 0) 0%, rgba(0, 0, 0, 0.08) 100%)',
        }}
      />

      {/* Left Navigation Rail (Floating icons, no vertical line, no bulky background) */}
      <NavRail
        activeView={activeView}
        onHomeClick={() => setActiveView('home')}
        onNewChatClick={() => {
          setActiveView('home');
        }}
        onProjectsClick={() => setProjectsTrigger((p) => p + 1)}
        onSettingsClick={() => setIsSettingsOpen((prev) => !prev)}
        isSettingsOpen={isSettingsOpen}
        isLight={isLight}
      />

      {/* Main Workspace Area */}
      <div className="relative flex-1 h-full overflow-hidden flex flex-col z-10">
        {/* Dynamic Island at Top Center */}
        <div className="fixed top-3 left-1/2 -translate-x-1/2 z-50 pointer-events-auto">
          <DynamicIsland
            projects={projects}
            activeTaskProjectName={activeTask?.projectName}
            activeTaskBranch={activeTask?.branch}
            usageReport={usage.report}
            usageError={usage.error}
            activeTasks={spawner.allActiveTasks}
            notification={islandNotification}
            onDismissNotification={() => {
              setIslandNotification(null);
            }}
            historyEntries={history.entries}
            activeWorkspaceId={spawner.workspaceId}
            onOpenHistory={handleOpenHistorySession}
            onDeleteHistory={history.remove}
            onSetChatArchived={(wsId, tId, arch) => {
              void history.setArchived(wsId, tId, arch);
            }}
            onMoveChat={(payload, proj) => {
              void spawner.moveChat(payload, proj).then(() => history.refresh());
            }}
            onRefreshHistory={history.refresh}
            openProjectsTrigger={projectsTrigger}
            openHistoryTrigger={historyTrigger}
            claudeSessions={claudeImport.sessions}
            claudeLoading={claudeImport.isLoading}
            onImportClaudeSession={(filePath) => {
              void (async () => {
                const targetIdx = await spawner.openHistorySession('', filePath);
                if (targetIdx >= 0) {
                  setActiveCardIndex(targetIdx);
                  setActiveView('chat');
                }
              })();
            }}
          />
        </div>

        {/* View Content */}
        {activeView === 'home' || !activeTask ? (
          /* Blank Chat / Home View */
          <div className="relative flex-1 w-full h-full flex flex-col items-center justify-between pt-24 pb-8 px-6 overflow-y-auto">
            {/* Centered Stats & Activity Heatmap */}
            <div className="w-full flex-1 flex items-center justify-center">
              <HomeStats isLight={isLight} />
            </div>

            {/* Bottom Liquid Glass Input */}
            <div className="w-full pb-2">
              <GlassInput
                onSubmit={handleSubmitPrompt}
                attachments={attachments}
                isLight={isLight}
              />
            </div>
          </div>
        ) : (
          /* Fullscreen Active Chat View */
          <div className="relative flex-1 w-full h-full overflow-hidden">
            <Suspense fallback={null}>
              <AgentWindow
                key={activeTask.threadId}
                id={activeTask.threadId}
                title={activeTask.title}
                subtitle={activeTask.branch}
                modelId={activeTask.model}
                driver={activeTask.driver}
                status={activeTask.isBusy ? 'working' : 'idle'}
                streamBlocks={activeTask.blocks}
                onSendMessage={(msg: string, mId: string, files?: domain.FileRef[]) =>
                  spawner.sendWithModel(activeTask.threadId, msg, files, mId)
                }
                onInterrupt={() => spawner.interrupt(activeTask.threadId)}
                onAnswerQuestion={async (blockId, answers) => {
                  const qId = blockId.replace(/^question-/, '');
                  await RespondToQuestion(activeTask.threadId, qId, answers);
                }}
                onSkipQuestion={async (blockId) => {
                  const qId = blockId.replace(/^question-/, '');
                  await RespondToQuestion(activeTask.threadId, qId, []);
                }}
                onDeleteTurn={(turnId) => spawner.deleteTurn(activeTask.threadId, turnId)}
                onClose={() => handleCloseTask(activeTask.threadId)}
                projectName={activeTask.projectName}
                projectPath={activeTask.projectPath}
                accountName={accountLabel(activeTask.driver, activeTask.account)}
                isFullscreen={true}
                className="w-full h-full"
              />
            </Suspense>
          </div>
        )}
      </div>

      {/* Settings Panel Flyout (Only for settings at the bottom) */}
      {isSettingsOpen && (
        <div className="fixed inset-y-0 left-[48px] w-[380px] z-50 shadow-2xl animate-slide-right flex flex-col bg-[#111215]/95 backdrop-blur-xl border-r border-white/10">
          <SettingsPanel
            wallpaper={wallpaper}
            defaultModels={defaultModels}
            onClose={() => setIsSettingsOpen(false)}
            className="w-full h-full"
          />
        </div>
      )}
    </div>
  );
};
