import { useEffect, useRef, useState, type ReactNode } from 'react';
import { Check, Plus, Save } from 'lucide-react';
import { Button, Modal } from './ui';
import { cx } from '../lib/format';
import { parseRuleTextWithStats, RULE_EXAMPLES, rulesToText } from '../lib/rules';

export function CustomRulesModal({
  open,
  onClose,
  title,
  description,
  rules,
  onSave,
}: {
  open: boolean;
  onClose: () => void;
  title: string;
  description?: ReactNode;
  rules: string[];
  onSave: (rules: string[]) => Promise<void>;
}) {
  const [text, setText] = useState('');
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [inserted, setInserted] = useState<string | null>(null);
  const gutterRef = useRef<HTMLDivElement>(null);
  const textareaRef = useRef<HTMLTextAreaElement>(null);

  useEffect(() => {
    if (!open) {
      setInserted(null);
      return;
    }
    setText(rulesToText(rules));
    setError(null);
    setInserted(null);
    // 仅在打开时同步，避免父组件轮询刷新覆盖编辑中的文本。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  const { rules: parsed, duplicateCount } = parseRuleTextWithStats(text);
  const dirty = parsed.join('\n') !== rules.join('\n');
  const lineNumbers = Array.from({ length: text.split('\n').length }, (_, index) => index + 1);

  const insertExample = (rule: string) => {
    setText((current) => (current === '' ? rule : `${rule}\n${current}`));
    setInserted(rule);
    window.setTimeout(() => {
      setInserted((current) => (current === rule ? null : current));
    }, 1500);
    requestAnimationFrame(() => {
      const area = textareaRef.current;
      if (!area) return;
      area.focus();
      area.setSelectionRange(0, 0);
      area.scrollTop = 0;
      if (gutterRef.current) gutterRef.current.scrollTop = 0;
    });
  };

  const submit = async () => {
    setSaving(true);
    setError(null);
    try {
      await onSave(parsed);
      onClose();
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setSaving(false);
    }
  };

  return (
    <Modal
      open={open}
      onClose={saving ? () => undefined : onClose}
      title={title}
      description={description}
      width="max-w-5xl"
      footer={
        <>
          <span className="mr-auto text-[11px] text-slate-500">
            共 {parsed.length} 条规则
            {duplicateCount > 0 && (
              <span className="ml-2 text-amber-300/80">已自动去重 {duplicateCount} 条</span>
            )}
          </span>
          <Button variant="ghost" onClick={onClose} disabled={saving}>
            取消
          </Button>
          <Button
            variant="primary"
            loading={saving}
            disabled={!dirty}
            onClick={() => void submit()}
          >
            <Save className="h-3.5 w-3.5" />
            保存并应用
          </Button>
        </>
      }
    >
      <div className="grid gap-3 md:h-[24rem] md:grid-cols-[minmax(0,1fr)_20rem]">
        <div className="flex min-h-0 flex-col overflow-hidden rounded-lg border border-line bg-ink-950/60 transition focus-within:border-emerald-500/60 focus-within:ring-2 focus-within:ring-emerald-500/10">
          <div className="flex items-center justify-between gap-3 border-b border-line/70 px-3 py-2">
            <span className="text-xs font-medium text-slate-300">规则编辑</span>
            <span className="text-[11px] text-slate-600">{lineNumbers.length} 行</span>
          </div>
          <div className="flex min-h-0 flex-1">
            <div
              ref={gutterRef}
              aria-hidden="true"
              className="mono shrink-0 select-none overflow-hidden border-r border-line/70 bg-ink-950/60 py-2 text-right text-xs leading-5 text-slate-600"
              style={{
                width: `calc(${Math.max(2, String(lineNumbers.length).length)}ch + 1.5rem)`,
              }}
            >
              <div className="px-2">
                {lineNumbers.map((line) => (
                  <div key={line}>{line}</div>
                ))}
              </div>
            </div>
            <textarea
              ref={textareaRef}
              value={text}
              onChange={(event) => setText(event.target.value)}
              onScroll={(event) => {
                if (gutterRef.current) {
                  gutterRef.current.scrollTop = event.currentTarget.scrollTop;
                }
              }}
              rows={16}
              wrap="off"
              spellCheck={false}
              placeholder="在此输入扩展规则（每行一条）"
              className="mono min-w-0 flex-1 resize-none bg-transparent px-3 py-2 text-xs leading-5 text-slate-200 placeholder:text-slate-600 focus:outline-none"
            />
          </div>
        </div>

        <aside className="flex max-h-[24rem] min-h-0 flex-col overflow-hidden rounded-lg border border-line bg-ink-950/60 md:max-h-none">
          <div className="flex items-center justify-between gap-3 border-b border-line/70 px-3 py-2">
            <span className="text-xs font-medium text-slate-300">规则示例</span>
            <span className="text-[11px] text-slate-600">{RULE_EXAMPLES.length} 条</span>
          </div>
          <div className="min-h-0 flex-1 space-y-1.5 overflow-y-auto p-2">
            {RULE_EXAMPLES.map((example) => {
              const isInserted = inserted === example.rule;
              return (
                <div
                  key={example.rule}
                  className="rounded-md border border-line/70 bg-white/[0.015] px-2.5 py-2 transition hover:border-emerald-500/30"
                >
                  <p className="text-[11px] leading-relaxed text-slate-500">{example.description}</p>
                  <div className="mt-1 flex items-start gap-2">
                    <code className="mono min-w-0 flex-1 break-all text-[11px] leading-relaxed text-emerald-300">
                      {example.rule}
                    </code>
                    <button
                      type="button"
                      onClick={() => insertExample(example.rule)}
                      className={cx(
                        'inline-flex shrink-0 items-center gap-1 rounded-md border px-2 py-1 text-[11px] transition',
                        isInserted
                          ? 'border-emerald-500/30 bg-emerald-500/10 text-emerald-300'
                          : 'border-line-strong bg-white/[0.02] text-slate-400 hover:text-slate-200',
                      )}
                      title="插入到第一行"
                    >
                      {isInserted ? <Check className="h-3 w-3" /> : <Plus className="h-3 w-3" />}
                      {isInserted ? '已插入' : '插入'}
                    </button>
                  </div>
                </div>
              );
            })}
          </div>
        </aside>
      </div>
      <div className="mt-3 space-y-1 rounded-lg border border-line bg-ink-950/40 px-3 py-2.5 text-[11px] leading-relaxed text-slate-500">
        <p>
          每行一条 mihomo 规则，格式为「类型,内容,策略」；右侧示例点击「插入」会添加到编辑框第一行。
        </p>
        <p>
          自定义规则会插入在订阅规则之前，优先命中；不支持 MATCH（会屏蔽后续订阅规则）。空行与
          # 开头的注释行会被忽略，重复规则会自动去重。
        </p>
        <p>
          策略需使用 DIRECT、REJECT 等内置策略，或当前订阅中实际存在的代理组 / 节点名称；保存时会校验策略，不存在时会指出具体规则。
        </p>
        <p className="text-amber-200/70">
          示例中的 PROXY 为代理组名称示例，插入后请确认当前订阅中存在该代理组，或替换为实际名称。
        </p>
      </div>
      {error && (
        <p className="mt-3 rounded-lg border border-rose-500/30 bg-rose-500/5 px-3 py-2 text-xs text-rose-300">
          {error}
        </p>
      )}
    </Modal>
  );
}
