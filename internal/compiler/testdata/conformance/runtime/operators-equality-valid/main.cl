class Main inherits IO {
  emit(value : Bool) : Object { if value then out_string("T") else out_string("F") fi };
  main() : Object {{
    emit(1 = 1); emit(1 = 2);
    emit("same" = "same"); emit("left" = "right");
    emit(true = true); emit(true = false);
    let same : Object <- new Object, other : Object <- new Object, empty : Object in {
      emit(same = same); emit(same = other); emit(empty = empty);
      emit(empty = same); emit(isvoid empty); emit(isvoid same);
    };
    out_string("/"); out_int(~1);
    out_string("/"); out_int(1 + 2 * 3 - 4 / 2);
    out_string("/"); emit(1 < 2); emit(2 <= 2); emit(not false);
  }};
};
