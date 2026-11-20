import {
  type ButtonHTMLAttributes,
  type InputHTMLAttributes,
  type ReactNode,
  type SelectHTMLAttributes,
  type TextareaHTMLAttributes,
  useEffect,
  useId,
  useRef,
  useState,
} from 'react';
import { createPortal } from 'react-dom';
import { Check, Copy, Loader2, X } from 'lucide-react';
import { cx } from '../lib/format';

type Variant = 'primary' | 'outline' | 'ghost' | 'danger';

interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: Variant;
  size?: 'sm' | 'md';
  loading?: boolean;
}

const VARIANTS: Record<Variant, string> = {
  primary: 'btn-primary',
  outline: 'btn-outline',
  ghost: 'btn-ghost',
  danger: 'btn-danger',
};

export function Button({
  variant = 'outline',
  size = 'md',
  loading = false,
  className,
  disabled,
  children,
  type = 'button',
  ...rest
}: ButtonProps) {
  return (
    <button
      type={type}
      disabled={disabled || loading}
      className={cx('btn', VARIANTS[variant], size === 'sm' && 'btn-sm', className)}
      {...rest}
    >
      {loading && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
      {children}
    </button>
  );
}

export function Input({ className, ...rest }: InputHTMLAttributes<HTMLInputElement>) {
  return <input className={cx('input', className)} {...rest} />;
}

export function Textarea({ className, ...rest }: TextareaHTMLAttributes<HTMLTextAreaElement>) {
  return <textarea className={cx('input mono text-xs leading-relaxed', className)} {...rest} />;
}

export function Select({ className, children, ...rest }: SelectHTMLAttributes<HTMLSelectElement>) {
  return (
    <select className={cx('input pr-8', className)} {...rest}>
      {children}
    </select>
  );
}

export function Field({
  label,
  hint,
  children,
  className,
  htmlFor,
}: {
  label?: string;
  hint?: ReactNode;
  children: ReactNode;
  className?: string;
  htmlFor?: string;
}) {
  return (
    <div className={className}>
      {label && (
        <label className="label" htmlFor={htmlFor}>
          {label}
        </label>
      )}
      {children}
      {hint && <p className="hint">{hint}</p>}
    </div>
  );
}

export function Toggle({
  checked,
  onChange,
  disabled,
  label,
}: {
  checked: boolean;
  onChange: (value: boolean) => void;
  disabled?: boolean;
  label?: string;
}) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      aria-label={label}
      disabled={disabled}
      onClick={() => onChange(!checked)}
      className={cx(
        'relative inline-flex h-6 w-11 shrink-0 items-center rounded-full transition-colors',
        checked ? 'bg-emerald-500' : 'bg-slate-700',
        disabled && 'cursor-not-allowed opacity-50',
      )}
    >
      <span
        className={cx(
          'inline-block h-5 w-5 transform rounded-full bg-[#fff] shadow transition-transform',
          checked ? 'translate-x-5' : 'translate-x-0.5',
        )}
      />
    </button>
  );
}

type BadgeTone = 'slate' | 'emerald' | 'amber' | 'rose' | 'sky' | 'violet';

const TONES: Record<BadgeTone, string> = {
  slate: 'border-slate-600/40 bg-slate-500/10 text-slate-300',
  emerald: 'border-emerald-500/30 bg-emerald-500/10 text-emerald-300',
  amber: 'border-amber-500/30 bg-amber-500/10 text-amber-300',
  rose: 'border-rose-500/30 bg-rose-500/10 text-rose-300',
  sky: 'border-sky-500/30 bg-sky-500/10 text-sky-300',
  violet: 'border-violet-500/30 bg-violet-500/10 text-violet-300',
};

export function Badge({
  tone = 'slate',
  children,
  className,
}: {
  tone?: BadgeTone;
  children: ReactNode;
  className?: string;
}) {
  return <span className={cx('badge', TONES[tone], className)}>{children}</span>;
}

export function Modal({
  open,
  onClose,
  title,
  description,
  children,
  footer,
  width = 'max-w-lg',
}: {
  open: boolean;
  onClose: () => void;
  title: ReactNode;
  description?: ReactNode;
  children: ReactNode;
  footer?: ReactNode;
  width?: string;
}) {
  useEffect(() => {
    if (!open) return;
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') onClose();
    };
    document.addEventListener('keydown', onKey);
    const previous = document.body.style.overflow;
    document.body.style.overflow = 'hidden';
    return () => {
      document.removeEventListener('keydown', onKey);
      document.body.style.overflow = previous;
    };
  }, [open, onClose]);

  if (!open) return null;

  return createPortal(
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4">
      <div
        className="absolute inset-0 bg-black/65 backdrop-blur-sm"
        onClick={onClose}
        aria-hidden="true"
      />
      <div
        className={cx(
          'animate-in relative z-10 flex max-h-[88vh] w-full flex-col overflow-hidden rounded-2xl border border-line bg-ink-900 shadow-2xl',
          width,
        )}
        role="dialog"
        aria-modal="true"
      >
        <div className="flex items-start justify-between gap-4 border-b border-line px-5 py-4">
          <div>
            <h3 className="text-base font-semibold text-slate-100">{title}</h3>
            {description && <p className="mt-1 text-xs text-slate-500">{description}</p>}
          </div>
          <button
            type="button"
            onClick={onClose}
            className="rounded-lg p-1 text-slate-500 transition hover:bg-white/5 hover:text-slate-200"
            aria-label="关闭"
          >
            <X className="h-4 w-4" />
          </button>
        </div>
        <div className="min-h-0 flex-1 overflow-y-auto px-5 py-4">{children}</div>
        {footer && (
          <div className="flex items-center justify-end gap-2 border-t border-line bg-ink-950/40 px-5 py-3.5">
            {footer}
          </div>
        )}
      </div>
    </div>,
    document.body,
  );
}

export function ConfirmDialog({
  open,
  title,
  description,
  confirmText = '确认',
  danger = false,
  busy = false,
  onCancel,
  onConfirm,
}: {
  open: boolean;
  title: string;
  description?: ReactNode;
  confirmText?: string;
  danger?: boolean;
  busy?: boolean;
  onCancel: () => void;
  onConfirm: () => void;
}) {
  return (
    <Modal
      open={open}
      onClose={onCancel}
      title={title}
      width="max-w-md"
      footer={
        <>
          <Button variant="ghost" onClick={onCancel} disabled={busy}>
            取消
          </Button>
          <Button variant={danger ? 'danger' : 'primary'} onClick={onConfirm} loading={busy}>
            {confirmText}
          </Button>
        </>
      }
    >
      <p className="text-sm leading-relaxed text-slate-400">{description}</p>
    </Modal>
  );
}

export function ConfirmPopover({
  open,
  message,
  detail,
  confirmText = '确认',
  danger = false,
  busy = false,
  placement = 'bottom',
  className,
  onCancel,
  onConfirm,
}: {
  open: boolean;
  message: ReactNode;
  detail?: ReactNode;
  confirmText?: string;
  danger?: boolean;
  busy?: boolean;
  placement?: 'top' | 'bottom';
  className?: string;
  onCancel: () => void;
  onConfirm: () => void;
}) {
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    const onPointerDown = (event: PointerEvent) => {
      if (ref.current && !ref.current.contains(event.target as Node)) onCancel();
    };
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') onCancel();
    };
    document.addEventListener('pointerdown', onPointerDown);
    document.addEventListener('keydown', onKey);
    return () => {
      document.removeEventListener('pointerdown', onPointerDown);
      document.removeEventListener('keydown', onKey);
    };
  }, [open, onCancel]);

  if (!open) return null;

  return (
    <div
      ref={ref}
      role="dialog"
      className={cx(
        'absolute right-0 z-30 w-60 rounded-xl border border-line bg-ink-900 p-3 shadow-2xl',
        placement === 'top' ? 'bottom-full mb-2' : 'top-full mt-2',
        className,
      )}
    >
      <span
        className={cx(
          'absolute right-5 h-2 w-2 rotate-45 border-line bg-ink-900',
          placement === 'top' ? '-bottom-1 border-r border-b' : '-top-1 border-t border-l',
        )}
      />
      <p className="text-xs font-medium text-slate-200">{message}</p>
      {detail && <p className="mt-0.5 text-[10px] leading-relaxed text-slate-500">{detail}</p>}
      <div className="mt-2.5 flex justify-end gap-1.5">
        <Button size="sm" variant="ghost" onClick={onCancel} disabled={busy}>
          取消
        </Button>
        <Button size="sm" variant={danger ? 'danger' : 'primary'} onClick={onConfirm} loading={busy}>
          {confirmText}
        </Button>
      </div>
    </div>
  );
}

export function Spinner({ className }: { className?: string }) {
  return <Loader2 className={cx('h-4 w-4 animate-spin text-slate-400', className)} />;
}

export function EmptyState({
  icon,
  title,
  description,
  action,
}: {
  icon?: ReactNode;
  title: string;
  description?: ReactNode;
  action?: ReactNode;
}) {
  return (
    <div className="flex flex-col items-center justify-center gap-3 px-6 py-16 text-center">
      {icon && <div className="text-slate-600">{icon}</div>}
      <div>
        <p className="text-sm font-medium text-slate-300">{title}</p>
        {description && (
          <p className="mx-auto mt-1 max-w-md text-xs leading-relaxed text-slate-500">
            {description}
          </p>
        )}
      </div>
      {action}
    </div>
  );
}

export function CopyButton({
  value,
  className,
  label = '复制',
}: {
  value: string;
  className?: string;
  label?: string;
}) {
  const [copied, setCopied] = useState(false);
  return (
    <button
      type="button"
      title={label}
      onClick={async () => {
        try {
          await navigator.clipboard.writeText(value);
          setCopied(true);
          window.setTimeout(() => setCopied(false), 1500);
        } catch {
          setCopied(false);
        }
      }}
      className={cx(
        'inline-flex items-center gap-1 rounded-md border border-line-strong bg-white/[0.02] px-2 py-1 text-[11px] text-slate-400 transition hover:text-slate-200',
        className,
      )}
    >
      {copied ? <Check className="h-3 w-3 text-emerald-400" /> : <Copy className="h-3 w-3" />}
      {copied ? '已复制' : label}
    </button>
  );
}

export function Segmented<T extends string>({
  value,
  options,
  onChange,
  className,
}: {
  value: T;
  options: { value: T; label: ReactNode }[];
  onChange: (value: T) => void;
  className?: string;
}) {
  return (
    <div
      className={cx(
        'inline-flex items-center gap-0.5 rounded-lg border border-line bg-ink-950/60 p-0.5',
        className,
      )}
    >
      {options.map((option) => (
        <button
          key={option.value}
          type="button"
          onClick={() => onChange(option.value)}
          className={cx(
            'rounded-md px-2.5 py-1 text-xs font-medium transition',
            value === option.value
              ? 'bg-emerald-500/15 text-emerald-300 shadow-[inset_0_0_0_1px_rgba(52,211,153,0.25)]'
              : 'text-slate-400 hover:text-slate-200',
          )}
        >
          {option.label}
        </button>
      ))}
    </div>
  );
}

export function ProgressBar({
  value,
  className,
  tone = 'emerald',
}: {
  value: number;
  className?: string;
  tone?: 'emerald' | 'amber' | 'rose';
}) {
  const colors = {
    emerald: 'bg-emerald-500',
    amber: 'bg-amber-500',
    rose: 'bg-rose-500',
  };
  return (
    <div className={cx('h-1.5 w-full overflow-hidden rounded-full bg-white/10', className)}>
      <div
        className={cx('h-full rounded-full transition-all duration-300', colors[tone])}
        style={{ width: `${Math.max(0, Math.min(100, value))}%` }}
      />
    </div>
  );
}

export function Section({
  title,
  description,
  actions,
  children,
  className,
  bodyClassName,
}: {
  title?: ReactNode;
  description?: ReactNode;
  actions?: ReactNode;
  children: ReactNode;
  className?: string;
  bodyClassName?: string;
}) {
  return (
    <section className={cx('card overflow-hidden', className)}>
      {(title || actions) && (
        <header className="flex flex-wrap items-center justify-between gap-3 border-b border-line px-5 py-3.5">
          <div>
            {title && <h2 className="text-sm font-semibold text-slate-200">{title}</h2>}
            {description && <p className="mt-0.5 text-xs text-slate-500">{description}</p>}
          </div>
          {actions && <div className="flex flex-wrap items-center gap-2">{actions}</div>}
        </header>
      )}
      <div className={cx('p-5', bodyClassName)}>{children}</div>
    </section>
  );
}

export function SwitchRow({
  title,
  description,
  checked,
  onChange,
  disabled,
}: {
  title: string;
  description?: ReactNode;
  checked: boolean;
  onChange: (value: boolean) => void;
  disabled?: boolean;
}) {
  const id = useId();
  return (
    <div className="flex items-start justify-between gap-4">
      <div className="min-w-0">
        <label htmlFor={id} className="text-sm font-medium text-slate-300">
          {title}
        </label>
        {description && <p className="mt-0.5 text-xs leading-relaxed text-slate-500">{description}</p>}
      </div>
      <Toggle checked={checked} onChange={onChange} disabled={disabled} label={title} />
    </div>
  );
}
