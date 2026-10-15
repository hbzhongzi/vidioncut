<script setup lang="ts">
import { computed } from 'vue'

const props = defineProps<{ text: string }>()

// 文案以「错误/失败」开头 → 错误样式；「完成」→ 成功样式；其余信息样式
const tone = computed(() => {
  if (/^(错误|失败|提交失败|取消失败|探测失败|打开文件夹失败)/.test(props.text)) return 'err'
  if (/^(完成|批量裁剪结束|合并完成|已添加)/.test(props.text)) return 'ok'
  return 'info'
})
</script>

<template>
  <div v-if="text" class="msg" :class="tone">
    <span class="ic"></span>
    <span class="tx">{{ text }}</span>
  </div>
</template>

<style scoped>
.msg {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 12px;
  border-radius: 8px;
  padding: 8px 12px;
  border: 1px solid var(--border);
  background: var(--panel2);
  color: var(--text2);
  word-break: break-all;
}
.msg .ic {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  flex: none;
  background: var(--text3);
}
.msg.err {
  color: var(--danger);
  border-color: rgba(242, 96, 106, 0.4);
  background: rgba(242, 96, 106, 0.08);
}
.msg.err .ic {
  background: var(--danger);
}
.msg.ok {
  color: var(--success);
  border-color: rgba(63, 185, 111, 0.4);
  background: rgba(63, 185, 111, 0.08);
}
.msg.ok .ic {
  background: var(--success);
}
.msg.info .ic {
  background: var(--accent);
}
</style>
