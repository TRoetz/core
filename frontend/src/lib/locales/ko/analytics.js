/** Korean (ko) translations. */
export default {
  "analytics": {
    "title": "분석",
    "subtitle": "반복 주기 없이도 확인하는 작업 흐름과 납품 현황",
    "loading": "분석 불러오는 중...",
    "noData": "사용 가능한 데이터가 없습니다",
    "errorTitle": "분석을 불러오지 못했습니다",
    "unsupportedVersion": "서버가 지원하지 않는 분석 형식을 반환했습니다. 배포가 끝나면 새로고침하세요.",
    "collectionLoadError": "컬렉션을 불러오지 못했습니다. 모든 워크스페이스 작업을 분석에 표시합니다.",
    "retry": "다시 시도",
    "dateRange": "날짜 범위",
    "collection": "컬렉션",
    "allItems": "모든 워크스페이스 작업",
    "from": "시작",
    "to": "대상",
    "daysValue": "{value}일",
    "items_one": "작업 {count}개",
    "items_other": "작업 {count}개",
    "range": {
      "last30Days": "최근 30일",
      "last12Weeks": "최근 12주",
      "last6Months": "최근 6개월",
      "lastYear": "최근 1년",
      "custom": "사용자 정의"
    },
    "validation": {
      "invalid": "유효한 시작일과 종료일을 입력하세요.",
      "reversed": "시작일은 종료일 이전이거나 같아야 합니다.",
      "too_long": "366일 이내의 날짜 범위를 선택하세요."
    },
    "scope": {
      "summary": "현재 작업 {items}개 · {from}–{to}",
      "currentWorkspace": "현재 워크스페이스 작업 집합",
      "currentWorkspaceNote": "날짜 범위는 흐름 및 납품 차트에 적용됩니다. 상태 및 경과 시간은 현재 시점 기준입니다. 과거 차트도 현재 이 워크스페이스에 있는 작업을 사용하며, 이동하거나 삭제된 작업은 포함하지 않습니다.",
      "currentCollection": "현재 컬렉션 작업 집합",
      "currentCollectionNote": "날짜 범위는 흐름 및 납품 차트에 적용됩니다. 상태 및 경과 시간은 현재 시점 기준입니다. 과거 차트도 현재 이 컬렉션에 해당하는 작업을 사용하므로 컬렉션을 변경하면 분석 대상도 달라질 수 있습니다."
    },
    "health": {
      "title": "확인 필요",
      "description": "자세히 살펴볼 필요가 있는 현재 미완료 작업입니다.",
      "unfinished": "미완료",
      "overdue": "기한 초과",
      "stale": "장기 미활동",
      "staleHint": "{days}일 이상 활동 없음",
      "unassigned": "미배정",
      "withoutPriority": "우선순위 없음",
      "withoutEstimate": "추정치 없음",
      "attentionItems": "검토할 작업",
      "item": "작업",
      "status": "상태",
      "age": "경과 시간",
      "signals": "주의 신호",
      "flags": {
        "overdue": "기한 초과",
        "stale": "장기 미활동",
        "unassigned": "미배정",
        "without_priority": "우선순위 없음",
        "without_estimate": "추정치 없음"
      },
      "allClear": "현재 주의 신호에 해당하는 미완료 작업이 없습니다."
    },
    "throughput": {
      "title": "생성 및 완료 비교",
      "description": "주별 생성 및 최초 완료 현황입니다. 작업을 다시 열어도 최초 완료 기록은 바뀌지 않습니다.",
      "created": "생성됨",
      "completed": "완료",
      "net": "순변동",
      "average": "주당 평균 완료",
      "period": "기간",
      "definition": "완료는 완료 상태로 처음 전환된 시점을 의미합니다."
    },
    "aging": {
      "title": "진행 중 작업의 경과 시간",
      "description": "현재 미완료 작업이 생성된 후 지난 시간입니다.",
      "total": "활성 작업",
      "median": "경과 시간 중앙값",
      "p85": "85백분위수",
      "ageBand": "경과 시간 구간",
      "itemCount": "작업",
      "byStatus": "상태별 경과 시간",
      "oldest": "가장 오래된 미완료 작업",
      "status": "상태",
      "noActive": "이 범위에 미완료 작업이 없습니다.",
      "buckets": {
        "0_7": "0~7일",
        "8_14": "8~14일",
        "15_30": "15~30일",
        "31_60": "31~60일",
        "61_plus": "61일 이상"
      }
    },
    "deliveryTime": {
      "title": "완료 소요 시간",
      "description": "생성부터 최초 완료까지의 시간을 완료 주별로 표시합니다.",
      "analyzed": "완료된 작업",
      "average": "평균",
      "median": "중앙값",
      "p85": "85백분위수",
      "period": "완료 기간",
      "completed": "완료",
      "slowest": "완료까지 가장 오래 걸린 작업",
      "completedDate": "최초 완료",
      "duration": "완료 소요 시간",
      "missingHistory": "완료 이력이 없어 현재 완료된 작업 {count}개를 제외했습니다.",
      "missingHistory_one": "완료 이력이 없어 현재 완료된 작업 1개를 제외했습니다.",
      "missingHistory_other": "완료 이력이 없어 현재 완료된 작업 {count}개를 제외했습니다.",
      "definition": "작업 생성부터 완료 상태로 처음 전환될 때까지 측정합니다. 이후 다시 열어도 값은 바뀌지 않습니다."
    },
    "dataTable": {
      "show": "데이터 표 보기"
    },
    "insufficientData": {
      "no_items": "이 범위에 아직 작업이 없습니다.",
      "no_active_items": "이 범위에 미완료 작업이 없습니다.",
      "no_completed_items": "선택한 기간에 최초 완료 기록이 없습니다.",
      "few_completed_items": "이 기간의 완료 작업이 적습니다. 백분위수는 참고용으로 사용하세요."
    }
  }
};
