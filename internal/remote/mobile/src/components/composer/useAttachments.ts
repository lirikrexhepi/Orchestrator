import { useRef, useState, type RefObject } from 'react'
import { api } from '../../api'
import type { FileRef } from '../../types'
import { shrinkImage } from './shrinkImage'

export interface Attachment {
  ref: FileRef
  preview: string
}

const KEYBOARD_CLOSE_MS = 420
const MAX_FILES = 4

export function useAttachments(field: RefObject<HTMLTextAreaElement>) {
  const [files, setFiles] = useState<Attachment[]>([])
  const [uploading, setUploading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const input = useRef<HTMLInputElement | null>(null)

  const open = () => {
    const picker = input.current
    if (!picker) return
    const text = field.current
    if (text && document.activeElement === text) {
      text.blur()
      window.setTimeout(() => picker.click(), KEYBOARD_CLOSE_MS)
      return
    }
    picker.click()
  }

  const add = async (list: FileList | File[] | null) => {
    if (!list || list.length === 0) return
    setUploading(true)
    setError(null)
    try {
      for (const file of Array.from(list).slice(0, MAX_FILES)) {
        const { data, mime } = await shrinkImage(file)
        const ref = await api.upload(file.name || 'photo.jpg', mime, data)
        const preview = mime.startsWith('image/') ? data : ''
        setFiles((all) => [...all, { ref, preview }])
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Upload failed')
    } finally {
      setUploading(false)
      if (input.current) input.current.value = ''
    }
  }

  const remove = (index: number) => setFiles((all) => all.filter((_, i) => i !== index))
  const clear = () => setFiles([])

  return { files, uploading, error, input, open, add, remove, clear }
}
