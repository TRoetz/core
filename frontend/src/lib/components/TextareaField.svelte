<script>
  import Label from './Label.svelte';
  import Textarea from './Textarea.svelte';

  /**
   * @type {{
   *   label?: string,
   *   value?: any,
   *   placeholder?: string,
   *   required?: boolean,
   *   disabled?: boolean,
   *   autofocus?: boolean,
   *   spellcheck?: boolean,
   *   rows?: number,
   *   id?: string,
   *   size?: string,
   *   labelColor?: 'default' | 'subtle',
   *   labelClass?: string,
   *   textareaRef?: any,
   *   dataTestid?: string,
   *   oninput?: (e?: any) => void,
   *   class?: string,
   * }}}
   */
  let {
    label = '',
    value = $bindable(''),
    placeholder = '',
    required = false,
    disabled = false,
    autofocus = false,
    spellcheck = undefined,
    rows = 3,
    id = undefined,
    size = 'medium',
    labelColor = 'subtle',
    labelClass = 'mb-2',
    textareaRef = $bindable(null),
    dataTestid = undefined,
    oninput = undefined,
    class: className = ''
  } = $props();
</script>

<div class={className}>
  {#if label}
    <Label for={id} {required} color={labelColor} class={labelClass}>{label}</Label>
  {/if}
  <Textarea
    {id}
    bind:value
    bind:textareaRef
    {placeholder}
    {required}
    {disabled}
    {autofocus}
    {spellcheck}
    {rows}
    {size}
    data-testid={dataTestid}
    {oninput}
  />
</div>
