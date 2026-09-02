// Visitor-facing pages are the only screens a non-Korean speaker reaches, so
// the dictionary covers the mobile pass and the self pre-registration form.
// Staff screens stay in Korean, matching how the product is operated.
export type Locale = "ko" | "en" | "ja" | "zh";

export const localeNames: Record<Locale, string> = {
  ko: "한국어",
  en: "English",
  ja: "日本語",
  zh: "中文",
};

export const localeTags: Record<Locale, string> = {
  ko: "ko-KR",
  en: "en-US",
  ja: "ja-JP",
  zh: "zh-CN",
};

type Dictionary = {
  passTitle: string;
  visitor: string;
  companyMissing: string;
  scanHere: string;
  visitTime: string;
  place: string;
  host: string;
  departmentMissing: string;
  privacyNote: string;
  passNotFound: string;
  language: string;
  registrationTitle: string;
  registrationIntro: string;
  registrationDone: string;
  registrationDoneNote: string;
  name: string;
  company: string;
  jobTitle: string;
  email: string;
  vehicle: string;
  equipment: string;
  equipmentHelp: string;
  consent: string;
  consentDetail: string;
  nda: string;
  safety: string;
  submit: string;
  submitting: string;
  required: string;
  purpose: string;
  policyVersion: string;
  statusLabels: Record<string, string>;
};

const dictionaries: Record<Locale, Dictionary> = {
  ko: {
    passTitle: "모바일 방문증",
    visitor: "방문자",
    companyMissing: "회사 미입력",
    scanHere: "로비에 이 QR을 제시해 주세요 · 서버 실시간 검증",
    visitTime: "방문 일시",
    place: "방문 장소",
    host: "담당자",
    departmentMissing: "부서 미지정",
    privacyNote: "QR에는 이름·전화번호 같은 개인정보가 들어 있지 않습니다. 취소·재발급 시 즉시 무효화됩니다.",
    passNotFound: "유효한 모바일 방문증을 찾을 수 없습니다.",
    language: "언어",
    registrationTitle: "방문자 사전등록",
    registrationIntro: "아래 정보를 직접 확인하고 개인정보 수집·이용에 동의해 주세요.",
    registrationDone: "사전등록이 완료되었습니다",
    registrationDoneNote: "방문 당일 담당자가 안내한 모바일 방문증으로 입장해 주세요.",
    name: "이름",
    company: "회사명",
    jobTitle: "직책",
    email: "이메일",
    vehicle: "차량번호",
    equipment: "반입 장비",
    equipmentHelp: "노트북, 카메라처럼 쉼표로 구분해 입력하세요.",
    consent: "개인정보 수집·이용에 동의합니다.",
    consentDetail: "이름, 연락처, 회사 정보는 방문 관리와 출입 안전을 위해 사용되며 보존 기간이 지나면 파기됩니다.",
    nda: "보안서약 내용을 확인했습니다.",
    safety: "안전교육 이수를 확인했습니다.",
    submit: "사전등록 완료",
    submitting: "제출 중…",
    required: "필수 항목을 확인하세요.",
    purpose: "방문 목적",
    policyVersion: "동의 정책 버전",
    statusLabels: { SCHEDULED: "방문 예정", ARRIVED: "도착", CHECKED_IN: "방문 중", CHECKED_OUT: "퇴실 완료", CANCELLED: "취소", REJECTED: "반려", NO_SHOW: "미방문", EXPIRED: "기간 만료", PENDING_APPROVAL: "승인 대기" },
  },
  en: {
    passTitle: "Mobile visitor pass",
    visitor: "Visitor",
    companyMissing: "Company not provided",
    scanHere: "Show this QR code at the lobby — verified live by the server",
    visitTime: "Visit time",
    place: "Location",
    host: "Host",
    departmentMissing: "Department not set",
    privacyNote: "The QR code carries no personal data. It is revoked immediately when the visit is cancelled or the pass is reissued.",
    passNotFound: "This visitor pass is no longer valid.",
    language: "Language",
    registrationTitle: "Visitor pre-registration",
    registrationIntro: "Please confirm your details and consent to the processing of your personal data.",
    registrationDone: "Pre-registration complete",
    registrationDoneNote: "On the day of your visit, enter using the mobile pass your host sends you.",
    name: "Full name",
    company: "Company",
    jobTitle: "Job title",
    email: "Email",
    vehicle: "Vehicle plate",
    equipment: "Equipment brought on site",
    equipmentHelp: "Separate items with commas, for example: laptop, camera.",
    consent: "I consent to the collection and use of my personal data.",
    consentDetail: "Your name, contact details and company are used for visitor management and site safety, and are destroyed once the retention period ends.",
    nda: "I have read the confidentiality undertaking.",
    safety: "I confirm I have completed the safety briefing.",
    submit: "Complete pre-registration",
    submitting: "Submitting…",
    required: "Please complete the required fields.",
    purpose: "Purpose",
    policyVersion: "Consent policy version",
    statusLabels: { SCHEDULED: "Scheduled", ARRIVED: "Arrived", CHECKED_IN: "On site", CHECKED_OUT: "Checked out", CANCELLED: "Cancelled", REJECTED: "Rejected", NO_SHOW: "No show", EXPIRED: "Expired", PENDING_APPROVAL: "Awaiting approval" },
  },
  ja: {
    passTitle: "モバイル入館証",
    visitor: "訪問者",
    companyMissing: "会社名未入力",
    scanHere: "受付でこのQRコードをご提示ください（サーバーでリアルタイム検証）",
    visitTime: "訪問日時",
    place: "訪問場所",
    host: "担当者",
    departmentMissing: "部署未設定",
    privacyNote: "QRコードに氏名や電話番号は含まれません。キャンセルや再発行の際は即時無効になります。",
    passNotFound: "有効な入館証が見つかりません。",
    language: "言語",
    registrationTitle: "訪問者事前登録",
    registrationIntro: "以下の情報をご確認のうえ、個人情報の取得・利用にご同意ください。",
    registrationDone: "事前登録が完了しました",
    registrationDoneNote: "当日は担当者からご案内するモバイル入館証でご入館ください。",
    name: "氏名",
    company: "会社名",
    jobTitle: "役職",
    email: "メールアドレス",
    vehicle: "車両番号",
    equipment: "持ち込み機器",
    equipmentHelp: "ノートPC、カメラのようにカンマで区切って入力してください。",
    consent: "個人情報の取得・利用に同意します。",
    consentDetail: "氏名・連絡先・会社情報は訪問管理と入館安全のために利用し、保存期間の経過後に廃棄します。",
    nda: "秘密保持事項を確認しました。",
    safety: "安全教育の受講を確認しました。",
    submit: "事前登録を完了する",
    submitting: "送信中…",
    required: "必須項目をご確認ください。",
    purpose: "訪問目的",
    policyVersion: "同意ポリシー版",
    statusLabels: { SCHEDULED: "訪問予定", ARRIVED: "到着", CHECKED_IN: "訪問中", CHECKED_OUT: "退館済み", CANCELLED: "キャンセル", REJECTED: "却下", NO_SHOW: "未訪問", EXPIRED: "期限切れ", PENDING_APPROVAL: "承認待ち" },
  },
  zh: {
    passTitle: "移动访客证",
    visitor: "访客",
    companyMissing: "未填写公司",
    scanHere: "请在前台出示此二维码 · 服务器实时校验",
    visitTime: "来访时间",
    place: "来访地点",
    host: "接待人",
    departmentMissing: "未指定部门",
    privacyNote: "二维码不包含姓名、电话等个人信息。取消或重新签发后立即失效。",
    passNotFound: "未找到有效的访客证。",
    language: "语言",
    registrationTitle: "访客预登记",
    registrationIntro: "请确认以下信息，并同意收集与使用您的个人信息。",
    registrationDone: "预登记已完成",
    registrationDoneNote: "来访当天请使用接待人发送的移动访客证入场。",
    name: "姓名",
    company: "公司名称",
    jobTitle: "职务",
    email: "电子邮箱",
    vehicle: "车牌号",
    equipment: "携带设备",
    equipmentHelp: "请用逗号分隔，例如：笔记本电脑、相机。",
    consent: "我同意收集和使用我的个人信息。",
    consentDetail: "姓名、联系方式和公司信息用于访客管理与场所安全，保存期满后将被销毁。",
    nda: "我已阅读保密承诺内容。",
    safety: "我确认已完成安全培训。",
    submit: "完成预登记",
    submitting: "提交中…",
    required: "请填写必填项。",
    purpose: "来访目的",
    policyVersion: "同意政策版本",
    statusLabels: { SCHEDULED: "预约来访", ARRIVED: "已到达", CHECKED_IN: "访问中", CHECKED_OUT: "已离开", CANCELLED: "已取消", REJECTED: "已驳回", NO_SHOW: "未到访", EXPIRED: "已过期", PENDING_APPROVAL: "待审批" },
  },
};

export function isLocale(value: string | undefined | null): value is Locale {
  return value === "ko" || value === "en" || value === "ja" || value === "zh";
}

export function translations(locale: string | undefined): Dictionary {
  return dictionaries[isLocale(locale) ? locale : "ko"];
}

export function formatDateTime(value: string, locale: string | undefined) {
  const tag = localeTags[isLocale(locale) ? locale : "ko"];
  return new Date(value).toLocaleString(tag, { dateStyle: "medium", timeStyle: "short" });
}

export function formatTime(value: string, locale: string | undefined) {
  const tag = localeTags[isLocale(locale) ? locale : "ko"];
  return new Date(value).toLocaleTimeString(tag, { hour: "2-digit", minute: "2-digit" });
}
