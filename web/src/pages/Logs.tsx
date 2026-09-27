import { api } from '../lib/api'
import { useFetch } from '../lib/useFetch'
import { Card, CardTitle, Empty, ErrorBox } from '../components/ui/card'

export function LogsPage() {
  const { data, error, loading, reload } = useFetch(() => api.logs(200), [])
  return (
    <Card>
      <CardTitle>hook log (tail)</CardTitle>
      {loading ? (
        <div className="text-sm text-[#8a8aa0]">loading…</div>
      ) : error || !data ? (
        <ErrorBox message={error ?? 'no data'} onRetry={reload} />
      ) : data.lines.length === 0 ? (
        <Empty text="log is empty" />
      ) : (
        <pre className="max-h-[60vh] overflow-auto rounded bg-void p-2 font-terminal text-xs whitespace-pre-wrap">
          {data.lines.join('\n')}
        </pre>
      )}
    </Card>
  )
}
