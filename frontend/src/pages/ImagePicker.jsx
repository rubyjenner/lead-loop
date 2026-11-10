import { useState } from 'react'
import { shrinkImage } from '../image.js'

// Pick a picture, shrink it in the browser and hand back a data URL. value = current data URL ('' = none).
export default function ImagePicker({ value, onChange, label, hint, disabled }) {
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  async function pick(e) {
    const file = e.target.files?.[0]
    e.target.value = '' // lets the same file be chosen again after removing it
    if (!file) return
    setError(''); setBusy(true)
    try { onChange(await shrinkImage(file)) } catch (err) { setError(err.message) } finally { setBusy(false) }
  }
  return (
    <div className="field img-picker">
      <span>{label}</span>
      {value ? (
        <div className="img-row">
          <img src={value} alt="รูปที่เลือก" />
          <button type="button" className="btn btn-quiet" disabled={disabled} onClick={() => onChange('')}>เอารูปออก</button>
        </div>
      ) : (
        <label className="btn btn-quiet img-btn" aria-disabled={disabled || busy}>
          {busy ? 'กำลังย่อรูป…' : 'เลือกรูป'}
          <input type="file" accept="image/jpeg,image/png,image/webp" hidden disabled={disabled || busy} onChange={pick} />
        </label>
      )}
      {hint && <small>{hint}</small>}
      {error && <small className="err" role="alert">{error}</small>}
    </div>
  )
}
