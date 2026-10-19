import React, { useState } from 'react';
import { motion, AnimatePresence } from 'motion/react';
import { X, SlidersHorizontal, Cpu, Palette, Smartphone, ScrollText } from '../common/icons';
import { WallpaperState } from './useWallpaper';
import { DefaultModels } from './useDefaultModels';
import { useTheme } from '../../themes';
import { ProvidersSection } from './ProvidersSection';
import { RemoteAccessSection } from './RemoteAccessSection';
import { InstructionsSection } from './InstructionsSection';
import { useClaudeUpdate } from '../orchestrator/useClaudeUpdate';
import { ScrollArea } from '../common/ScrollArea';
import { AppIconPicker, FontPicker, GeneralSettings, SectionLabel, ThemePicker, WallpaperPicker } from './SettingsParts';

type SectionId = 'general' | 'providers' | 'instructions' | 'appearance' | 'remote';

const SECTIONS: { id: SectionId; label: string; icon: React.ReactNode }[] = [
  { id: 'general', label: 'General', icon: <SlidersHorizontal size={15} strokeWidth={1.9} /> },
  { id: 'providers', label: 'Providers', icon: <Cpu size={15} strokeWidth={1.9} /> },
  { id: 'instructions', label: 'Instructions & memory', icon: <ScrollText size={15} strokeWidth={1.9} /> },
  { id: 'appearance', label: 'Appearance', icon: <Palette size={15} strokeWidth={1.9} /> },
  { id: 'remote', label: 'Phone access', icon: <Smartphone size={15} strokeWidth={1.9} /> },
];

export function SettingsWindow({
  open,
  onClose,
  wallpaper,
  defaultModels,
}: {
  open: boolean;
  onClose: () => void;
  wallpaper: WallpaperState;
  defaultModels: DefaultModels;
}) {
  const { currentTheme } = useTheme();
  const isLight = currentTheme.id === 'light' || currentTheme.id === 'white';
  const isGlass = currentTheme.id === 'glass';
  const claudeUpdate = useClaudeUpdate();
  const [section, setSection] = useState<SectionId>('general');
  const fg = isLight ? 'text-black/90' : 'text-white';

  return (
    <AnimatePresence>
      {open && (
        <motion.div
          key="settings-window"
          className="fixed inset-0 z-[60] flex items-center justify-center pointer-events-auto"
          initial={{ opacity: 0 }}
          animate={{ opacity: 1 }}
          exit={{ opacity: 0, transition: { duration: 0.16 } }}
          onMouseDown={(event) => {
            if (event.target === event.currentTarget) onClose();
          }}
          style={{ backgroundColor: isLight ? 'rgba(255,255,255,0.18)' : 'rgba(0,0,0,0.35)' }}
        >
          <motion.div
            initial={{ scale: 0.94, y: 14, opacity: 0 }}
            animate={{ scale: 1, y: 0, opacity: 1 }}
            exit={{ scale: 0.96, y: 8, opacity: 0, transition: { duration: 0.14 } }}
            transition={{ type: 'spring', stiffness: 380, damping: 32, mass: 0.9 }}
            className={`w-[min(880px,calc(100vw-120px))] h-[min(640px,calc(100vh-180px))] rounded-[28px] overflow-hidden flex font-(family-name:--app-font) select-none border ${
              isLight ? 'border-black/10' : 'border-white/[0.08]'
            }`}
            style={{
              backgroundColor: isLight ? 'rgba(255,255,255,0.96)' : isGlass ? 'rgba(10,14,26,0.72)' : 'rgba(8,8,9,0.96)',
              boxShadow: isLight
                ? '0 30px 80px rgba(0,0,0,0.18), 0 6px 20px rgba(0,0,0,0.08)'
                : '0 30px 90px rgba(0,0,0,0.75), 0 6px 24px rgba(0,0,0,0.5)',
              backdropFilter: isGlass ? 'blur(18px) saturate(150%)' : undefined,
            }}
          >
            <nav className={`w-[210px] shrink-0 flex flex-col gap-0.5 p-3 pt-5 border-r ${isLight ? 'border-black/[0.06]' : 'border-white/[0.06]'}`}>
              <span className={`text-[15px] font-semibold tracking-tight px-3 pb-4 ${fg}`}>Settings</span>
              {SECTIONS.map((item) => {
                const active = item.id === section;
                return (
                  <button
                    key={item.id}
                    type="button"
                    onClick={() => setSection(item.id)}
                    className={`relative h-[36px] px-3 rounded-[11px] flex items-center gap-2.5 text-[13px] tracking-tight cursor-pointer transition-colors duration-150 ${
                      active
                        ? fg
                        : isLight
                          ? 'text-black/55 hover:text-black hover:bg-black/[0.04]'
                          : 'text-white/50 hover:text-white hover:bg-white/[0.05]'
                    }`}
                  >
                    {active && (
                      <motion.span
                        layoutId="settings-nav-pill"
                        className={`absolute inset-0 rounded-[11px] ${isLight ? 'bg-black/[0.07]' : 'bg-white/[0.09]'}`}
                        transition={{ type: 'spring', stiffness: 520, damping: 40 }}
                      />
                    )}
                    <span className="relative flex items-center gap-2.5">
                      {item.icon}
                      {item.label}
                    </span>
                  </button>
                );
              })}
            </nav>

            <div className="flex-1 min-w-0 flex flex-col">
              <div className="flex items-center justify-between px-7 pt-5 pb-3 shrink-0">
                <span className={`text-[17px] font-semibold tracking-tight ${fg}`}>
                  {SECTIONS.find((s) => s.id === section)?.label}
                </span>
                <button
                  type="button"
                  title="Close"
                  onClick={onClose}
                  className={`w-[30px] h-[30px] rounded-full flex items-center justify-center active:scale-90 transition-all duration-150 cursor-pointer ${
                    isLight ? 'bg-black/[0.05] hover:bg-black/[0.1] text-black/60' : 'bg-white/[0.07] hover:bg-white/[0.13] text-white/70'
                  }`}
                >
                  <X size={15} strokeWidth={2} />
                </button>
              </div>
              <ScrollArea className="flex-1 min-h-0 px-7 pb-7">
                <AnimatePresence mode="wait" initial={false}>
                  <motion.div
                    key={section}
                    initial={{ opacity: 0, y: 6 }}
                    animate={{ opacity: 1, y: 0 }}
                    exit={{ opacity: 0, y: -4, transition: { duration: 0.08 } }}
                    transition={{ duration: 0.18, ease: [0.16, 1, 0.3, 1] }}
                    className="flex flex-col gap-3 max-w-[560px]"
                  >
                    {section === 'general' && <GeneralSettings isLight={isLight} />}
                    {section === 'providers' &&
                      (defaultModels.entries.length > 0 ? (
                        <ProvidersSection defaultModels={defaultModels} claudeUpdate={claudeUpdate} isLight={isLight} />
                      ) : (
                        <span className={`text-[12.5px] ${isLight ? 'text-black/45' : 'text-white/40'}`}>Loading providers…</span>
                      ))}
                    {section === 'appearance' && (
                      <>
                        <SectionLabel isLight={isLight}>Theme</SectionLabel>
                        <ThemePicker isLight={isLight} />
                        <div className="h-2" />
                        <AppIconPicker isLight={isLight} />
                        <div className="h-2" />
                        <FontPicker isLight={isLight} />
                        <div className="h-2" />
                        <WallpaperPicker wallpaper={wallpaper} isLight={isLight} columns={4} />
                      </>
                    )}
                    {section === 'instructions' && <InstructionsSection isLight={isLight} />}
                    {section === 'remote' && <RemoteAccessSection />}
                  </motion.div>
                </AnimatePresence>
              </ScrollArea>
            </div>
          </motion.div>
        </motion.div>
      )}
    </AnimatePresence>
  );
}

export default SettingsWindow;
