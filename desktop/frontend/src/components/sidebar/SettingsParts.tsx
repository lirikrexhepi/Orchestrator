import { MaterialIcon } from '../common/icons';
import React, { useEffect, useRef, useState } from 'react';
import { WallpaperState } from './useWallpaper';
import { isCustom } from './wallpapers';
import { useTheme } from '../../themes';
import { useOrchestratorStore } from '../orchestrator/useOrchestratorStore';
import { GetAppIcon, GetUserPreference, SetAppIcon, SetUserPreference } from '../../../wailsjs/go/main/App';
import { APP_ICONS, DEFAULT_APP_ICON } from '../../appIcons';
import { BackgroundRows } from './BackgroundSection';
import { addFontFiles, applyFont, currentFontChoice, FONT_PRESETS, fontStack, isFontAvailable, loadFontCss, removeFamily, storedFaces } from '../../fonts';
import { AppUpdateState } from './useAppUpdate';

export function Switch({ on, onChange, isLight, label }: { on: boolean; onChange: () => void; isLight: boolean; label: string }) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={on}
      aria-label={label}
      onClick={onChange}
      className={`w-9 h-5 rounded-full p-0.5 transition-colors duration-200 ease-out cursor-pointer shrink-0 ${
        on ? (isLight ? 'bg-[#007AFF]' : 'bg-white/90') : isLight ? 'bg-black/15' : 'bg-white/15'
      }`}
    >
      <div
        className={`w-4 h-4 rounded-full transition-transform duration-200 ease-out ${
          on
            ? isLight
              ? 'translate-x-4 bg-white shadow-sm'
              : 'translate-x-4 bg-black shadow-sm'
            : isLight
              ? 'translate-x-0 bg-white shadow-sm'
              : 'translate-x-0 bg-white/60'
        }`}
      />
    </button>
  );
}

export function SectionLabel({ children, isLight, action }: { children: React.ReactNode; isLight: boolean; action?: React.ReactNode }) {
  return (
    <div className="flex items-baseline justify-between gap-2 px-0.5 pt-1">
      <span className={`text-[11px] font-medium tracking-tight ${isLight ? 'text-black/45' : 'text-white/40'}`}>{children}</span>
      {action}
    </div>
  );
}

export function ThemePicker({ isLight }: { isLight: boolean }) {
  const { themeId, availableThemes, setTheme } = useTheme();
  return (
    <div className="grid grid-cols-3 gap-2">
      {availableThemes.map((t) => {
        const selected = t.id === themeId;
        return (
          <button
            key={t.id}
            type="button"
            onClick={() => setTheme(t.id)}
            className={`h-[34px] px-2.5 rounded-[11px] flex items-center gap-2 transition-all duration-150 cursor-pointer active:scale-95 ${
              selected
                ? isLight
                  ? 'bg-black/10 text-black ring-1 ring-black/10'
                  : 'bg-white/[0.16] text-white'
                : isLight
                  ? 'bg-black/[0.04] hover:bg-black/[0.07] text-black/70'
                  : 'bg-white/[0.05] hover:bg-white/[0.09] text-white/70'
            }`}
          >
            <span className="w-3.5 h-3.5 rounded-full shrink-0" style={{ background: t.previewGradient }} />
            <span className="text-[12px] font-medium tracking-tight truncate">{t.name}</span>
          </button>
        );
      })}
    </div>
  );
}

export function useGeneralToggles() {
  const store = useOrchestratorStore;
  const autoStartAgents = store((s) => s.autoStartAgents);
  const setAutoStartAgents = store((s) => s.setAutoStartAgents);
  const autoApprovePermissions = store((s) => s.autoApprovePermissions);
  const setAutoApprovePermissions = store((s) => s.setAutoApprovePermissions);
  const interfaceSounds = store((s) => s.interfaceSounds);
  const setInterfaceSounds = store((s) => s.setInterfaceSounds);
  const showProjectFavicons = store((s) => s.showProjectFavicons);
  const setShowProjectFavicons = store((s) => s.setShowProjectFavicons);
  const chatsFullscreen = store((s) => s.chatsFullscreen);
  const setChatsFullscreen = store((s) => s.setChatsFullscreen);
  return {
    autoStart: { on: autoStartAgents, toggle: () => setAutoStartAgents(!autoStartAgents) },
    autoApprove: { on: autoApprovePermissions, toggle: () => setAutoApprovePermissions(!autoApprovePermissions) },
    sounds: { on: interfaceSounds, toggle: () => setInterfaceSounds(!interfaceSounds) },
    favicons: { on: showProjectFavicons, toggle: () => setShowProjectFavicons(!showProjectFavicons) },
    chatsFullscreen: { on: chatsFullscreen, toggle: () => setChatsFullscreen(!chatsFullscreen) },
  };
}

export function SettingsGroup({ title, isLight, children }: { title?: string; isLight: boolean; children: React.ReactNode }) {
  return (
    <div className="flex flex-col gap-1.5">
      {title && (
        <span className={`px-1 text-[11px] font-medium tracking-tight ${isLight ? 'text-black/45' : 'text-white/40'}`}>{title}</span>
      )}
      <div
        className={`flex flex-col rounded-[14px] overflow-hidden divide-y ${
          isLight ? 'bg-black/[0.035] divide-black/[0.06]' : 'bg-white/[0.04] divide-white/[0.06]'
        }`}
      >
        {children}
      </div>
    </div>
  );
}

export function SettingsRow({
  title,
  hint,
  isLight,
  control,
  children,
}: {
  title: string;
  hint?: string;
  isLight: boolean;
  control?: React.ReactNode;
  children?: React.ReactNode;
}) {
  return (
    <div className="flex flex-col gap-1.5 px-3.5 py-3">
      <div className="flex items-center justify-between gap-4">
        <div className="flex flex-col min-w-0 gap-0.5">
          <span className={`text-[12.5px] font-medium tracking-tight ${isLight ? 'text-black/90' : 'text-white/90'}`}>{title}</span>
          {hint && <span className={`text-[11px] tracking-tight leading-snug ${isLight ? 'text-black/45' : 'text-white/40'}`}>{hint}</span>}
        </div>
        {control}
      </div>
      {children}
    </div>
  );
}

export function GeneralSettings({ isLight, appUpdate }: { isLight: boolean; appUpdate?: AppUpdateState }) {
  const toggles = useGeneralToggles();
  const [gpu, setGpu] = useState(true);
  const [gpuChanged, setGpuChanged] = useState(false);
  const [launchFullscreen, setLaunchFullscreen] = useState(false);
  const [restoreOpenChats, setRestoreOpenChats] = useState(false);

  useEffect(() => {
    GetUserPreference('disable_gpu_acceleration')
      .then((val) => setGpu(val !== 'true'))
      .catch(() => undefined);
    GetUserPreference('launch_fullscreen')
      .then((val) => setLaunchFullscreen(val === 'true'))
      .catch(() => undefined);
    GetUserPreference('restore_open_chats')
      .then((val) => setRestoreOpenChats(val === 'true'))
      .catch(() => undefined);
  }, []);

  const updateStatus = appUpdate?.status;
  const currentVersionLabel = updateStatus?.isDevBuild
    ? 'Development build'
    : updateStatus?.currentVersion
      ? `v${updateStatus.currentVersion}`
      : 'Orchestrator';
  const updateHint = (() => {
    if (appUpdate?.installing && appUpdate.progress) {
      if (appUpdate.progress.stage === 'verifying') return 'Verifying SHA-256 signature…';
      if (appUpdate.progress.stage === 'installing') return 'Installing update and restarting…';
      return `Downloading v${updateStatus?.latestVersion || ''} (${appUpdate.progress.percent ?? 0}%)…`;
    }
    if (appUpdate?.error) return appUpdate.error;
    if (updateStatus?.available && updateStatus.latestVersion) {
      return `v${updateStatus.latestVersion} is ready to install`;
    }
    if (updateStatus?.isDevBuild && updateStatus.latestVersion) {
      return `Latest release: v${updateStatus.latestVersion} (install a release build for auto-updates)`;
    }
    if (updateStatus?.checkedAt && !updateStatus.error) {
      return 'Up to date';
    }
    return 'Checks GitHub Releases automatically';
  })();

  return (
    <div className="flex flex-col gap-5">
      {appUpdate && (
        <SettingsGroup title="Updates" isLight={isLight}>
          <SettingsRow
            title={`Orchestrator · ${currentVersionLabel}`}
            hint={updateHint}
            isLight={isLight}
            control={
              updateStatus?.available ? (
                <button
                  type="button"
                  disabled={appUpdate.installing}
                  onClick={() => void appUpdate.install()}
                  className="h-[26px] px-3 rounded-full text-[11.5px] font-medium bg-[#007AFF] hover:bg-[#0A84FF] text-white active:scale-95 transition-all cursor-pointer disabled:opacity-50 shrink-0"
                >
                  {appUpdate.installing ? 'Updating…' : `Update to v${updateStatus.latestVersion}`}
                </button>
              ) : (
                <button
                  type="button"
                  disabled={appUpdate.checking || appUpdate.installing}
                  onClick={() => void appUpdate.checkNow()}
                  className={`h-[26px] px-3 rounded-full text-[11.5px] font-medium active:scale-95 transition-all cursor-pointer disabled:opacity-50 shrink-0 ${
                    isLight
                      ? 'bg-black/[0.06] hover:bg-black/[0.11] text-black/80'
                      : 'bg-white/[0.08] hover:bg-white/[0.14] text-white/85'
                  }`}
                >
                  {appUpdate.checking ? 'Checking…' : 'Check for updates'}
                </button>
              )
            }
          >
            {appUpdate.installing && appUpdate.progress && (
              <div className={`h-[4px] w-full rounded-full overflow-hidden mt-1 ${isLight ? 'bg-black/[0.08]' : 'bg-white/[0.10]'}`}>
                <div
                  className="h-full rounded-full bg-[#0A84FF] transition-[width] duration-200"
                  style={{ width: `${Math.max(4, appUpdate.progress.percent ?? 0)}%` }}
                />
              </div>
            )}
          </SettingsRow>
        </SettingsGroup>
      )}

      <SettingsGroup title="Agents" isLight={isLight}>
        <SettingsRow
          title="Auto-start agents"
          isLight={isLight}
          control={<Switch on={toggles.autoStart.on} onChange={toggles.autoStart.toggle} isLight={isLight} label="Auto-start agents" />}
        />
        <SettingsRow
          title="Auto-approve permissions"
          hint="Skip prompts for folders and commands"
          isLight={isLight}
          control={<Switch on={toggles.autoApprove.on} onChange={toggles.autoApprove.toggle} isLight={isLight} label="Auto-approve permissions" />}
        />
      </SettingsGroup>

      <SettingsGroup title="Interface" isLight={isLight}>
        <SettingsRow
          title="Interface sounds"
          hint="Chime when an agent finishes"
          isLight={isLight}
          control={<Switch on={toggles.sounds.on} onChange={toggles.sounds.toggle} isLight={isLight} label="Interface sounds" />}
        />
        <SettingsRow
          title="Project favicons"
          hint="Show each app's icon instead of a folder"
          isLight={isLight}
          control={<Switch on={toggles.favicons.on} onChange={toggles.favicons.toggle} isLight={isLight} label="Project favicons" />}
        />
        <SettingsRow
          title="Open chats in full screen"
          hint="Chats fill the window instead of floating over the wallpaper"
          isLight={isLight}
          control={<Switch on={toggles.chatsFullscreen.on} onChange={toggles.chatsFullscreen.toggle} isLight={isLight} label="Open chats in full screen" />}
        />
      </SettingsGroup>

      <SettingsGroup title="Startup and system" isLight={isLight}>
        <BackgroundRows isLight={isLight} />
        <SettingsRow
          title="Launch maximized"
          hint="Fills the screen without hiding the taskbar. Applies on next start"
          isLight={isLight}
          control={
            <Switch
              on={launchFullscreen}
              onChange={() => {
                const next = !launchFullscreen;
                setLaunchFullscreen(next);
                void SetUserPreference('launch_fullscreen', next ? 'true' : 'false');
              }}
              isLight={isLight}
              label="Launch maximized"
            />
          }
        />
        <SettingsRow
          title="Reopen open chats on startup"
          hint="Restore the chat tabs you left open when closing Orchestrator"
          isLight={isLight}
          control={
            <Switch
              on={restoreOpenChats}
              onChange={() => {
                const next = !restoreOpenChats;
                setRestoreOpenChats(next);
                void SetUserPreference('restore_open_chats', next ? 'true' : 'false');
              }}
              isLight={isLight}
              label="Reopen open chats on startup"
            />
          }
        />
        <SettingsRow
          title="GPU acceleration"
          hint={gpuChanged ? 'Restart Orchestrator to apply' : 'Turn off if the window crashes or flickers'}
          isLight={isLight}
          control={
            <Switch
              on={gpu}
              onChange={() => {
                const next = !gpu;
                setGpu(next);
                setGpuChanged(true);
                void SetUserPreference('disable_gpu_acceleration', next ? 'false' : 'true');
              }}
              isLight={isLight}
              label="GPU acceleration"
            />
          }
        />
      </SettingsGroup>
    </div>
  );
}

export function WallpaperPicker({ wallpaper, isLight, columns = 3 }: { wallpaper: WallpaperState; isLight: boolean; columns?: number }) {
  const fileRef = useRef<HTMLInputElement>(null);
  return (
    <div className="flex flex-col gap-2">
      <SectionLabel
        isLight={isLight}
        action={
          <button
            type="button"
            onClick={() => fileRef.current?.click()}
            className={`text-[11.5px] font-medium tracking-tight transition-colors cursor-pointer ${
              isLight ? 'text-black/60 hover:text-black' : 'text-white/55 hover:text-white'
            }`}
          >
            Upload
          </button>
        }
      >
        Wallpaper
      </SectionLabel>
      <input
        ref={fileRef}
        type="file"
        accept="image/*"
        className="hidden"
        onChange={(event) => {
          const file = event.target.files?.[0];
          if (file) void wallpaper.upload(file);
          event.target.value = '';
        }}
      />
      {wallpaper.error && (
        <div className="px-3 py-2 rounded-[10px] bg-amber-500/10 text-[11px] text-amber-100/90">{wallpaper.error}</div>
      )}
      <div className="grid gap-2" style={{ gridTemplateColumns: `repeat(${columns}, minmax(0, 1fr))` }}>
        {wallpaper.wallpapers.map((item) => {
          const selected = wallpaper.selected?.id === item.id;
          return (
            <div key={item.id} className="relative group">
              <button
                type="button"
                title={item.label}
                onClick={() => wallpaper.select(item.id)}
                className={`w-full aspect-[16/10] rounded-[10px] overflow-hidden transition-all duration-150 cursor-pointer active:scale-95 ${
                  selected
                    ? isLight
                      ? 'ring-2 ring-inset ring-black/80'
                      : 'ring-2 ring-inset ring-white/80'
                    : isLight
                      ? 'ring-1 ring-inset ring-black/15 hover:ring-black/40'
                      : 'ring-1 ring-inset ring-white/15 hover:ring-white/40'
                }`}
              >
                <img src={item.url} alt={item.label} loading="lazy" draggable={false} className="w-full h-full object-cover" />
              </button>
              {isCustom(item.id) && (
                <button
                  type="button"
                  title="Remove"
                  onClick={() => wallpaper.remove(item.id)}
                  className="absolute top-1 right-1 w-[18px] h-[18px] rounded-full bg-black/60 hover:bg-rose-500/80 opacity-0 group-hover:opacity-100 flex items-center justify-center transition-all duration-150 cursor-pointer"
                >
                  <MaterialIcon name="close" className="text-[12px] text-white"/>
                </button>
              )}
            </div>
          );
        })}
      </div>
    </div>
  );
}

export function FontPicker({ isLight }: { isLight: boolean }) {
  const [choice, setChoice] = useState(currentFontChoice);
  const [custom, setCustom] = useState<string[]>([]);
  const [available, setAvailable] = useState<Record<string, boolean>>({});
  const [error, setError] = useState<string | null>(null);
  const fileRef = useRef<HTMLInputElement>(null);

  const refresh = async () => {
    const faces = await storedFaces();
    const presetFamilies = new Set(FONT_PRESETS.map((p) => p.family));
    setCustom([...new Set(faces.map((f) => f.family))].filter((family) => !presetFamilies.has(family)));
    await document.fonts.ready;
    setAvailable({ Matter: isFontAvailable('Matter') });
  };

  useEffect(() => {
    for (const preset of FONT_PRESETS) loadFontCss(preset.css);
    void refresh();
  }, []);

  const pick = (next: string) => {
    applyFont(next);
    setChoice(next);
  };

  const upload = async (files: FileList | null) => {
    if (!files?.length) return;
    setError(null);
    try {
      const families = await addFontFiles(Array.from(files));
      if (families.length === 0) {
        setError('Choose .ttf, .otf or .woff2 files');
        return;
      }
      await refresh();
      const family = families[0];
      const preset = FONT_PRESETS.find((p) => p.family === family);
      pick(preset ? preset.id : `custom:${family}`);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
    }
  };

  const tile = (active: boolean) =>
    `relative h-[76px] rounded-[13px] flex flex-col items-start justify-between p-3 text-left transition-all duration-150 cursor-pointer active:scale-[0.97] ${
      active
        ? isLight
          ? 'bg-black/[0.09] ring-1 ring-black/15'
          : 'bg-white/[0.12] ring-1 ring-white/20'
        : isLight
          ? 'bg-black/[0.035] hover:bg-black/[0.06]'
          : 'bg-white/[0.04] hover:bg-white/[0.07]'
    }`;
  const fg = isLight ? 'text-black/90' : 'text-white/90';
  const sub = isLight ? 'text-black/45' : 'text-white/40';

  return (
    <div className="flex flex-col gap-2">
      <SectionLabel
        isLight={isLight}
        action={
          <button
            type="button"
            onClick={() => fileRef.current?.click()}
            className={`text-[11.5px] font-medium tracking-tight transition-colors cursor-pointer ${isLight ? 'text-black/60 hover:text-black' : 'text-white/55 hover:text-white'}`}
          >
            Upload font
          </button>
        }
      >
        Font
      </SectionLabel>
      <input
        ref={fileRef}
        type="file"
        multiple
        accept=".ttf,.otf,.woff,.woff2"
        className="hidden"
        onChange={(event) => {
          void upload(event.target.files);
          event.target.value = '';
        }}
      />
      <div className="grid grid-cols-3 gap-2">
        {FONT_PRESETS.map((preset) => {
          const missing = preset.id === 'matter' && available.Matter === false;
          return (
            <button
              key={preset.id}
              type="button"
              onClick={() => (missing ? fileRef.current?.click() : pick(preset.id))}
              className={tile(choice === preset.id)}
            >
              <span className={`text-[22px] leading-none tracking-tight ${missing ? sub : fg}`} style={{ fontFamily: fontStack(preset.family) }}>
                Aa
              </span>
              <span className="flex flex-col min-w-0">
                <span className={`text-[11.5px] font-medium tracking-tight truncate ${fg}`}>{preset.label}</span>
                {missing && <span className={`text-[10px] tracking-tight ${sub}`}>Upload its files</span>}
              </span>
            </button>
          );
        })}
        {custom.map((family) => (
          <div key={family} className="relative group">
            <button type="button" onClick={() => pick(`custom:${family}`)} className={`w-full ${tile(choice === `custom:${family}`)}`}>
              <span className={`text-[22px] leading-none tracking-tight ${fg}`} style={{ fontFamily: fontStack(family) }}>
                Aa
              </span>
              <span className={`text-[11.5px] font-medium tracking-tight truncate ${fg}`}>{family}</span>
            </button>
            <button
              type="button"
              title="Remove font"
              onClick={() => {
                void removeFamily(family).then(() => {
                  if (choice === `custom:${family}`) pick('geist');
                  void refresh();
                });
              }}
              className="absolute top-1.5 right-1.5 w-[20px] h-[20px] rounded-full bg-black/60 hover:bg-rose-500/80 text-white opacity-0 group-hover:opacity-100 flex items-center justify-center transition-all duration-150 cursor-pointer"
            >
              <MaterialIcon name="close" className="text-[12px]"/>
            </button>
          </div>
        ))}
      </div>
      {error && <span className="text-[11px] text-red-300/90">{error}</span>}
    </div>
  );
}

export function AppIconPicker({ isLight }: { isLight: boolean }) {
  const [current, setCurrent] = useState(DEFAULT_APP_ICON);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    GetAppIcon()
      .then((id) => setCurrent(id || DEFAULT_APP_ICON))
      .catch(() => undefined);
  }, []);

  const choose = (id: string) => {
    const previous = current;
    setCurrent(id);
    setError(null);
    SetAppIcon(id).catch((cause) => {
      setCurrent(previous);
      setError(cause instanceof Error ? cause.message : String(cause));
    });
  };

  return (
    <div className="flex flex-col gap-2">
      <SectionLabel isLight={isLight}>App icon</SectionLabel>
      <div className="grid grid-cols-6 gap-2">
        {APP_ICONS.map((icon) => {
          const active = icon.id === current;
          return (
            <button
              key={icon.id}
              type="button"
              title={icon.label}
              aria-label={icon.label}
              aria-pressed={active}
              onClick={() => choose(icon.id)}
              className={`aspect-square rounded-[16px] p-1.5 transition-all duration-150 cursor-pointer active:scale-95 ${
                active
                  ? isLight
                    ? 'ring-2 ring-black/70'
                    : 'ring-2 ring-white/80'
                  : isLight
                    ? 'hover:bg-black/[0.05]'
                    : 'hover:bg-white/[0.06]'
              }`}
            >
              <img src={icon.src} alt="" draggable={false} className="w-full h-full object-contain" />
            </button>
          );
        })}
      </div>
      <span className={`px-1 text-[11px] tracking-tight leading-snug ${isLight ? 'text-black/45' : 'text-white/40'}`}>
        Applies instantly to the window, taskbar, desktop and Start menu.
      </span>
      {error && <span className="px-1 text-[11px] text-red-300/90">{error}</span>}
    </div>
  );
}
