import { useEffect, useState } from 'react'
import { INSTALL_METHODS } from './site'

function CopyButton({ text }: { text: string }) {
  const [copied, setCopied] = useState(false)

  useEffect(() => {
    if (!copied) return
    const timer = setTimeout(() => setCopied(false), 1600)
    return () => clearTimeout(timer)
  }, [copied])

  return (
    <button
      type="button"
      onClick={() => navigator.clipboard.writeText(text).then(() => setCopied(true), () => {})}
    >
      {copied ? 'Copied' : 'Copy'}
    </button>
  )
}

interface CmdProps {
  text: string
  pre?: boolean
}

export function Cmd({ text, pre }: CmdProps) {
  const Text = pre ? 'pre' : 'code'
  return (
    <div className="cmd">
      <Text>{text}</Text>
      <CopyButton text={text} />
    </div>
  )
}

export function InstallTabs({ pre }: { pre?: boolean }) {
  const [pick, setPick] = useState(0)
  return (
    <>
      <div className="tabs" role="group" aria-label="Install method">
        {INSTALL_METHODS.map(([label], i) => (
          <button key={label} type="button" aria-pressed={i === pick} onClick={() => setPick(i)}>
            {label}
          </button>
        ))}
      </div>
      <Cmd text={INSTALL_METHODS[pick][1]} pre={pre} />
    </>
  )
}
