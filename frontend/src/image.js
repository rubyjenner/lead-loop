// Shrinks a picture in the browser before it is uploaded. The API accepts at most 600 KB per picture (the request body
// limit is 1 MB and the picture travels inside JSON as base64), and a 1280 px JPEG is plenty for the AI and the shop page.
const MAX_SIDE = 1280
const MAX_BYTES = 540 * 1024 // a little under the server limit

function load(file) {
  return new Promise((resolve, reject) => {
    const url = URL.createObjectURL(file)
    const img = new Image()
    img.onload = () => { URL.revokeObjectURL(url); resolve(img) }
    img.onerror = () => { URL.revokeObjectURL(url); reject(new Error('เปิดไฟล์รูปนี้ไม่ได้ ลองใช้ไฟล์ JPEG, PNG หรือ WebP')) }
    img.src = url
  })
}

// Returns a data URL ("data:image/jpeg;base64,...") no larger than MAX_BYTES.
export async function shrinkImage(file) {
  if (!file || !/^image\/(jpeg|png|webp)$/.test(file.type)) throw new Error('รองรับเฉพาะไฟล์ JPEG, PNG หรือ WebP')
  const img = await load(file)
  let scale = Math.min(1, MAX_SIDE / Math.max(img.width, img.height))
  for (let round = 0; round < 6; round++) {
    const w = Math.max(1, Math.round(img.width * scale)), h = Math.max(1, Math.round(img.height * scale))
    const canvas = document.createElement('canvas')
    canvas.width = w; canvas.height = h
    const ctx = canvas.getContext('2d')
    ctx.fillStyle = '#fff' // PNG with transparency would turn black as JPEG
    ctx.fillRect(0, 0, w, h)
    ctx.drawImage(img, 0, 0, w, h)
    for (const q of [0.85, 0.72, 0.6, 0.48]) {
      const url = canvas.toDataURL('image/jpeg', q)
      if ((url.length - url.indexOf(',') - 1) * 0.75 <= MAX_BYTES) return url
    }
    scale *= 0.75
  }
  throw new Error('ย่อรูปให้เล็กพอไม่ได้ ลองใช้รูปอื่น')
}
