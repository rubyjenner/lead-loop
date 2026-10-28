package main

import (
	"fmt"
	"strings"
)

// Rule/template fallback used when no AI provider is available. It only restates facts that were
// entered into the system (campaign, product, brand); it never adds promotions, prices or claims.

type campaignInfo struct {
	Name        string
	Goal        string
	Audience    string
	Budget      float64
	Product     string
	ProductDesc string
	Price       *float64
	Brand       string
}

// facts renders the data the AI may use (also sent to the AI providers).
func (ci campaignInfo) facts() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "แคมเปญ: %s\nเป้าหมาย: %s\nกลุ่มเป้าหมาย: %s\nงบประมาณ: %.0f บาท\n", ci.Name, ci.Goal, ci.Audience, ci.Budget)
	if ci.Product != "" {
		fmt.Fprintf(&sb, "สินค้า: %s\n", ci.Product)
		if ci.ProductDesc != "" {
			fmt.Fprintf(&sb, "รายละเอียดสินค้า: %s\n", ci.ProductDesc)
		}
		if ci.Price != nil {
			fmt.Fprintf(&sb, "ราคา: %.0f บาท\n", *ci.Price)
		}
		if ci.Brand != "" {
			fmt.Fprintf(&sb, "ข้อมูลแบรนด์: %s\n", ci.Brand)
		}
	} else {
		sb.WriteString("สินค้า: (ยังไม่ได้ระบุสินค้า)\n")
	}
	return sb.String()
}

func (ci campaignInfo) subject() string {
	if ci.Product != "" {
		return ci.Product
	}
	return ci.Name
}

func (ci campaignInfo) priceText() string {
	if ci.Price != nil {
		return fmt.Sprintf(" ราคา %.0f บาท", *ci.Price)
	}
	return ""
}

func orDefault(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}

// templatePlan builds a short campaign plan from the campaign data.
func templatePlan(ci campaignInfo) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "แผนแคมเปญ: %s (สร้างจากเทมเพลต ไม่ได้ใช้ AI ควรให้ทีมตรวจและปรับก่อนใช้)\n", ci.Name)
	fmt.Fprintf(&sb, "1. เป้าหมาย: %s\n", orDefault(ci.Goal, "ยังไม่ได้ระบุ (ควรกำหนดตัวเลข เช่น จำนวนลีด)"))
	fmt.Fprintf(&sb, "2. กลุ่มเป้าหมาย: %s\n", orDefault(ci.Audience, "ยังไม่ได้ระบุ"))
	if ci.Product != "" {
		fmt.Fprintf(&sb, "3. สินค้า: %s%s\n", ci.Product, ci.priceText())
	} else {
		sb.WriteString("3. สินค้า: ยังไม่ได้ระบุ (ควรเพิ่มสินค้าและข้อมูลแบรนด์ก่อนสร้างข้อความ)\n")
	}
	sb.WriteString("4. ช่องทางแนะนำ: LINE สำหรับตอบคำถามและเก็บลีด, โพสต์โซเชียลเพื่อสร้างการรับรู้\n")
	sb.WriteString("5. แนวทางข้อความ: ใช้เฉพาะข้อมูลสินค้าที่ระบุไว้ ชวนให้ทักสอบถาม ไม่โฆษณาเกินจริง\n")
	sb.WriteString("6. ตัวชี้วัด: reach, CTR, จำนวนลีด, ต้นทุนต่อลีด")
	if ci.Budget > 0 {
		fmt.Fprintf(&sb, " (งบประมาณ %.0f บาท)", ci.Budget)
	}
	sb.WriteString("\n7. ขั้นต่อไป: ทีมตรวจแผน สร้างข้อความ ส่งอนุมัติ แล้วจึงเผยแพร่")
	return sb.String()
}

// templateContents returns up to count (max 5) different ad drafts built from the entered facts.
func templateContents(ci campaignInfo, channel string, count int) []string {
	s := ci.subject()
	price := ci.priceText()
	detail := ""
	if ci.ProductDesc != "" {
		detail = "\n" + ci.ProductDesc
	}
	brand := ""
	if ci.Brand != "" {
		brand = "\n" + ci.Brand
	}
	audience := ""
	if ci.Audience != "" {
		audience = "สำหรับ" + ci.Audience + " "
	}
	ch := strings.ToUpper(channel)
	all := []string{
		fmt.Sprintf("%s%s%s\nสนใจสอบถามเพิ่มเติมได้เลย", s, price, detail),
		fmt.Sprintf("%s%s%s\nทักมาสอบถามหรือขอข้อมูลได้ทาง %s", audience, s, detail, ch),
		fmt.Sprintf("กำลังมองหา %s อยู่หรือเปล่า?%s\nส่งข้อความมาให้เราช่วยแนะนำได้", s, price),
		fmt.Sprintf("%s%s\nอยากรู้รายละเอียดเพิ่มเติม ทักมาได้เลย", s, brand),
		fmt.Sprintf("ติดตามข้อมูลของ %s ได้ที่นี่%s\nมีคำถามส่งข้อความมาได้เลย", s, price),
	}
	if count < 1 {
		count = 1
	}
	if count > len(all) {
		count = len(all)
	}
	return all[:count]
}

// planPrompt and contentsPrompt build the requests sent to the AI providers.
func planPrompt(ci campaignInfo) string {
	return "วางแผนแคมเปญสั้นๆ ประกอบด้วย กลุ่มลูกค้าย่อย ช่องทาง ข้อความหลัก และตัวชี้วัดที่ใช้วัดผล\n\n" + ci.facts()
}

func contentsPrompt(ci campaignInfo, channel string, count int) string {
	return fmt.Sprintf("เขียนข้อความโฆษณา %d แบบ สำหรับช่องทาง %s แต่ละแบบคั่นด้วยบรรทัดที่มีเฉพาะ --- ไม่ต้องมีหัวข้อหรือเลขข้อ ต้องเขียนข้อความโฆษณาให้ครบตามจำนวนเสมอ แม้ข้อมูลน้อยก็ให้เขียนแบบทั่วไปจากข้อมูลที่มี ห้ามถามกลับหรือขอข้อมูลเพิ่ม\n\n%s",
		count, channel, ci.facts())
}

// splitDrafts splits AI text into at most max drafts (separated by a line containing only ---).
func splitDrafts(text string, max int) []string {
	out := []string{}
	for _, part := range strings.Split(text, "\n---\n") {
		part = strings.Trim(strings.TrimSpace(part), "-")
		part = strings.TrimSpace(part)
		if part == "" || len(out) >= max {
			continue
		}
		out = append(out, part)
	}
	return out
}
