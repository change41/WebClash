import {
  createContext,
  useCallback,
  useContext,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from 'react';
import { CheckCircle2, CircleAlert, Info, X } from 'lucide-react';
import { cx } from './format';

type ToastKind = 'success' | 'error' | 'info';

interface ToastItem {
  id: number;
  kind: ToastKind;
  message: string;
}

interface ToastApi {
  push: (message: string, kind?: ToastKind) => void;
  success: (message: string) => void;
  error: (message: string) => void;
  info: (message: string) => void;
}

const ToastContext = createContext<ToastApi | null>(null);

const STYLES: Record<ToastKind, string> = {
  success: 'border-emerald-500/30 bg-emerald-500/10 text-emerald-100',
  error: 'border-rose-500/30 bg-rose-500/10 text-rose-100',
  info: 'border-sky-500/30 bg-sky-500/10 text-sky-100',
};

const ICONS: Record<ToastKind, ReactNode> = {
  success: <CheckCircle2 className="mt-0.5 h-4 w-4 shrink-0 text-emerald-400" />,
  error: <CircleAlert className="mt-0.5 h-4 w-4 shrink-0 text-rose-400" />,
  info: <Info className="mt-0.5 h-4 w-4 shrink-0 text-sky-400" />,
};

export function ToastProvider({ children }: { children: ReactNode }) {
  const [items, setItems] = useState<ToastItem[]>([]);
  const idRef = useRef(0);

  const remove = useCallback((id: number) => {
    setItems((list) => list.filter((item) => item.id !== id));
  }, []);

  const push = useCallback(
    (message: string, kind: ToastKind = 'info') => {
      const id = ++idRef.current;
      setItems((list) => [...list.slice(-4), { id, kind, message }]);
      window.setTimeout(() => remove(id), kind === 'error' ? 6000 : 3500);
    },
    [remove],
  );

  const api = useMemo<ToastApi>(
    () => ({
      push,
      success: (message) => push(message, 'success'),
      error: (message) => push(message, 'error'),
      info: (message) => push(message, 'info'),
    }),
    [push],
  );

  return (
    <ToastContext.Provider value={api}>
      {children}
      <div className="pointer-events-none fixed top-4 right-4 z-[100] flex w-[min(92vw,400px)] flex-col gap-2">
        {items.map((item) => (
          <div
            key={item.id}
            className={cx(
              'animate-toast pointer-events-auto flex items-start gap-2.5 rounded-lg border px-3.5 py-2.5 text-sm shadow-2xl backdrop-blur',
              STYLES[item.kind],
            )}
          >
            {ICONS[item.kind]}
            <span className="min-w-0 flex-1 break-words leading-relaxed">{item.message}</span>
            <button
              type="button"
              onClick={() => remove(item.id)}
              className="mt-0.5 rounded p-0.5 text-white/40 transition hover:text-white/80"
              aria-label="关闭"
            >
              <X className="h-3.5 w-3.5" />
            </button>
          </div>
        ))}
      </div>
    </ToastContext.Provider>
  );
}

export function useToast(): ToastApi {
  const ctx = useContext(ToastContext);
  if (!ctx) throw new Error('useToast 必须在 ToastProvider 内使用');
  return ctx;
}
