import { Moon, Sun } from 'lucide-react';
import { cx } from '../lib/format';
import { useTheme } from '../lib/theme';

export function ThemeToggle({ className }: { className?: string }) {
  const { theme, toggle } = useTheme();
  const dark = theme === 'dark';
  const label = dark ? '切换到浅色主题' : '切换到深色主题';

  return (
    <button
      type="button"
      onClick={toggle}
      title={label}
      aria-label={label}
      className={cx(
        'rounded-lg border border-line bg-ink-900/60 p-1.5 text-slate-400 transition hover:bg-white/5 hover:text-slate-200',
        className,
      )}
    >
      {dark ? <Sun className="h-4 w-4" /> : <Moon className="h-4 w-4" />}
    </button>
  );
}
